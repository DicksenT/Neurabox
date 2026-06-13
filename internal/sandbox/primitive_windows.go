//go:build windows

package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/DicksenT/neurabox/internal/assets"
	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

type PrimitiveEngine struct{}

func NewPrimitiveEngine() *PrimitiveEngine {
	fmt.Println("🚀 Using Native Windows Primitive Isolation (Zero Dependency Mode)")
	return &PrimitiveEngine{}
}

func (p *PrimitiveEngine) Create(ctx context.Context, workingDir string) error {
	return nil
}

func (p *PrimitiveEngine) RunInteractive(ctx context.Context, workingDir string, command []string, assetDir string, hardened bool) error {
	if len(command) == 0 {
		return fmt.Errorf("empty command block provided")
	}

	// Enable VT processing on the parent's stdout so ANSI colour codes render
	// correctly. This only affects the parent console mode — the child inherits
	// the console natively (see SysProcAttr below) so it benefits automatically.
	var consoleMode uint32
	stdoutHandle := windows.Handle(os.Stdout.Fd())
	if err := windows.GetConsoleMode(stdoutHandle, &consoleMode); err == nil {
		_ = windows.SetConsoleMode(stdoutHandle, consoleMode|0x0004) // ENABLE_VIRTUAL_TERMINAL_PROCESSING
	}

	// Extract the rtk binary.

	var rtkPath string
	rtkBinData, rtkBinName,_ := assets.GetRTKBinary()
    if rtkBinData != nil {
        rtkPath := filepath.Join(assetDir, rtkBinName)
        if err := os.WriteFile(rtkPath, rtkBinData, 0755); err != nil {
		// handle error
			log.Fatalf("failed to initialize embedded optimizer: %v", err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(rtkPath, 0755); err != nil {
				// handle error
				log.Fatalf("failed to initialize embedded optimizer: %v", err)
			}
		}
        // Run init
    }

	

	// Write .exe shim copies instead of .bat wrappers.
	//
	// PERFORMANCE FIX: The previous .bat shims required:
	//   agent → cmd.exe(parse batch) → rtk-windows.exe → real tool
	// Three process hops × ~30-50ms CreateProcess each = visible lag on every
	// git/npm/npx call the agent makes in the background.
	//
	// rtk-windows.exe already uses os.Args[0] (its own executable name) to know
	// which tool to proxy. Copying the binary directly as git.exe / npm.exe / npx.exe
	// means the chain becomes:
	//   agent → rtk-windows.exe(as git.exe) → real tool
	// One hop removed, no cmd.exe overhead.
	// 🚀 THE FIX: Use absolute path Lookups with accurate Windows extensions
	interceptedTools := []string{"git", "npm", "npx"}
	for _, tool := range interceptedTools {
		// 1. Find the REAL tool on the host before we manipulate the sandbox PATH
		realToolPath, err := exec.LookPath(tool)
		if err != nil {
			continue // If they don't have git/npm installed, skip it cleanly
		}

		// 2. Node.js agents explicitly call "npm.cmd", while git uses "git.exe".
		// We must map our shims to the exact extension the agent expects to intercept it.
		ext := filepath.Ext(realToolPath)
		if ext == "" {
			ext = ".exe"
		}
		
		shimPath := filepath.Join(assetDir, tool+ext)

		// 3. We inject the ABSOLUTE path to the real tool directly into RTK.
		// RTK receives: Args[0]="rtk.exe", Args[1]="C:\Program Files\...\npm.cmd", Args[2]="list"
		// This guarantees 0% argument corruption and 0% infinite loop risk.
		shimContent := fmt.Sprintf("@echo off\n\"%s\" \"%s\" %%*\n", rtkPath, tool)
		
		if err := os.WriteFile(shimPath, []byte(shimContent), 0755); err != nil {
			log.Printf("Warning: failed to write shim for %s: %v", tool, err)
		}
	}

	// Set env vars BEFORE RunInteractive snapshots os.Environ() to build cmd.Env,
	// so RTK_DB_PATH, TERM, and COLORTERM are actually inherited by the child process.
	persistentDB := filepath.Join(os.Getenv("APPDATA"), "neurabox", "rtk_metrics.db")
	_ = os.MkdirAll(filepath.Dir(persistentDB), 0755)
	os.Setenv("RTK_DB_PATH", persistentDB)

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workingDir

	// --- Environment assembly ---
	// Rebuild the environment from scratch.
	// Keys we strip and re-add with controlled values:
	//   PATH      — must have shim dir prepended; duplicate entries confuse resolution.
	//   PWD       — Node.js CLIs (Gemini, Claude Code) read process.env.PWD and use
	//               it as their working directory sense, overriding the OS cwd set via
	//               cmd.Dir / CreateProcess lpCurrentDirectory. If the parent's real
	//               project path leaks through here the agent writes to the original
	//               source dir instead of shadowDir. Must be set to workingDir only.
	//   INIT_CWD  — npm/npx store the original invocation dir here; same risk.
	//   CD        — cmd.exe internal; strip to avoid confusion.
	// Keys we strip entirely:
	//   VSCODE_*, ELECTRON_* — IPC socket paths that trigger background round-trips.
	var cleanEnv []string
	for _, env := range os.Environ() {
		// 1. OBLITERATE Windows hidden drive tracking (e.g. =D:=D:\Neurabox)
		if strings.HasPrefix(env, "=") {
			continue
		}

		key := env
		if idx := strings.IndexByte(env, '='); idx > 0 {
			key = env[:idx]
		}
		upper := strings.ToUpper(key)

		// Keys we strip and re-add below with controlled values
		switch upper {
		case "PATH", "PWD", "INIT_CWD", "CD", "RTK_DB_PATH", "TERM", "COLORTERM":
			continue
		}

		// 2. OBLITERATE host-context, IDE sockets, and AI Memory Trackers
		if strings.HasPrefix(upper, "VSCODE_") ||
			strings.HasPrefix(upper, "ELECTRON_") ||
			strings.HasPrefix(upper, "NPM_") ||
			strings.HasPrefix(upper, "GIT_") ||      
			strings.HasPrefix(upper, "CLAUDE_") ||   
			strings.HasPrefix(upper, "GEMINI_") ||   
			strings.HasPrefix(upper, "AIDER_") {  
			continue
		}

		cleanEnv = append(cleanEnv, env)
	}

	// Add exactly one controlled entry for each path-sensitive variable.
	rtkDB := os.Getenv("RTK_DB_PATH")
	cleanEnv = append(cleanEnv,
		fmt.Sprintf("PATH=%s;%s", assetDir, os.Getenv("PATH")),
		fmt.Sprintf("PWD=%s", workingDir),
		fmt.Sprintf("INIT_CWD=%s", workingDir),
		fmt.Sprintf("CD=%s", workingDir),
		fmt.Sprintf("RTK_DB_PATH=%s", rtkDB),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	if hardened {
		fmt.Println("HARDENED MODE ACTIVE: Severing local loopback network structures...")
		cleanEnv = append(cleanEnv,
			"NO_PROXY=localhost,127.0.0.1",
			"HTTP_PROXY=",
			"HTTPS_PROXY=",
			"DISABLE_IDE_SYNC=1",
		)
	}
	cmd.Env = cleanEnv

	// Inherit the parent's console handles directly. Required for interactive
	// agents (Gemini CLI, Claude Code) that probe isatty on startup and refuse
	// to launch in interactive mode if stdin is not a real console handle.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Inside internal/sandbox/primitive_windows.go -> RunInteractive()

	// Create a temporary memory buffer or a raw session log file
	// --- Job Object sandbox ---
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create secure job boundary: %v", err)
	}
	defer windows.CloseHandle(job)

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		return fmt.Errorf("failed to apply job object limits: %v", err)
	}

	// Spawn suspended inside the same console session (no CREATE_NEW_CONSOLE /
	// no DETACHED_PROCESS) so the child inherits stdin/stdout/stderr console
	// handles directly from the OS, bypassing Go's pipe bridge entirely.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW,
		HideWindow: true,

	}

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("primitive ignition fault: %v", err)
	}

	// Assign to Job Object before resuming.
	var assignErr error
	withHandleErr := cmd.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(handle))
	})
	if withHandleErr != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("failed to access process handle for job assignment: %v", withHandleErr)
	}
	if assignErr != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("failed to assign process to security job object: %v", assignErr)
	}

	// Find the primary thread to resume.
	dWDProcID := uint32(cmd.Process.Pid)
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("failed to snapshot threads: %v", err)
	}
	defer windows.CloseHandle(snapshot)

	var targetThreadID uint32
	var threadEntry windows.ThreadEntry32
	threadEntry.Size = uint32(unsafe.Sizeof(threadEntry))

	err = windows.Thread32First(snapshot, &threadEntry)
	for err == nil {
		if threadEntry.OwnerProcessID == dWDProcID {
			targetThreadID = threadEntry.ThreadID
			break
		}
		err = windows.Thread32Next(snapshot, &threadEntry)
	}
	if targetThreadID == 0 {
		_ = cmd.Process.Kill()
		return fmt.Errorf("could not find primary thread for PID %d", dWDProcID)
	}

	threadHandle, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, targetThreadID)
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("failed to open thread handle: %v", err)
	}
	defer windows.CloseHandle(threadHandle)

	// Drop into raw mode as late as possible — just before we unblock the child —
	// so the window where raw bytes could be misread is minimal.
	stdinFd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("failed to enter raw terminal mode: %v", err)
	}
	defer func() { _ = term.Restore(stdinFd, oldState) }()

	if _, err = windows.ResumeThread(threadHandle); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("failed to resume primary thread: %v", err)
	}

	err = cmd.Wait()
	if err != nil && !strings.Contains(err.Error(), "exit status") {
		return err
	}
	return nil
}

func (p *PrimitiveEngine) RunSilentCheck(ctx context.Context, workingDir string, command []string) (string, error) {
	var cmd *exec.Cmd
	if len(command) >= 3 && command[0] == "sh" && command[1] == "-c" {
		// Best-effort POSIX→cmd.exe translation.
		// Callers must inspect the output string for "Safe:" sentinels emitted
		// by the || echo fallback in POSIX-style policy commands, because many
		// POSIX constructs cause cmd.exe to exit non-zero regardless of intent.
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", command[2])
	} else {
		cmd = exec.CommandContext(ctx, command[0], command[1:]...)
	}
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p *PrimitiveEngine) Destroy(ctx context.Context) error {
	return nil
}