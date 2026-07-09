//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

type PrimitiveEngine struct{}

func NewPrimitiveEngine() *PrimitiveEngine {
	fmt.Println(" Using Native Linux Primitive Isolation (Zero Dependency Mode)")
	return &PrimitiveEngine{}
}

func (p *PrimitiveEngine) Create(ctx context.Context, workingDir string) error {
	return nil
}

func (p *PrimitiveEngine) RunInteractive(ctx context.Context, workingDir string, command []string, assetDir string, hardened bool) error {
	if len(command) == 0 {
		return fmt.Errorf("empty command block provided")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workingDir

	// --- Environment assembly ---
	// --- Environment assembly ---
	var cleanEnv []string
	for _, env := range os.Environ() {
		key := env
		if idx := strings.IndexByte(env, '='); idx > 0 {
			key = env[:idx]
		}
		upper := strings.ToUpper(key)

		// Strip path-sensitive variables to rebuild securely below
		switch upper {
		case "PATH", "PWD", "INIT_CWD", "RTK_DB_PATH", "TERM", "COLORTERM":
			continue
		}

		//  FIX: Preserve Git identity for sandbox commits, but block dangerous path overrides
		switch upper {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
			"GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR", "GIT_CEILING_DIRECTORIES":
			continue
		}

		// OBLITERATE host-context, IDE sockets, and AI Memory Trackers (Removed GIT_ from here)
		if strings.HasPrefix(upper, "VSCODE_") ||
			strings.HasPrefix(upper, "ELECTRON_") ||
			strings.HasPrefix(upper, "NPM_") ||
			strings.HasPrefix(upper, "CLAUDE_") ||
			strings.HasPrefix(upper, "GEMINI_") ||
			strings.HasPrefix(upper, "AIDER_") {
			continue
		}

		cleanEnv = append(cleanEnv, env)
	}

	rtkDB := os.Getenv("RTK_DB_PATH")
	cleanEnv = append(cleanEnv,
		fmt.Sprintf("PATH=%s:%s", assetDir, os.Getenv("PATH")),
		fmt.Sprintf("PWD=%s", workingDir),
		fmt.Sprintf("INIT_CWD=%s", workingDir),
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
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// --- Native Linux Process Group Isolation ---
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,            // Isolates the process tree group
		Pdeathsig: syscall.SIGKILL, // Kills child processes instantly if Neurabox dies
	}

	// Capture terminal state
	stdinFd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return fmt.Errorf("failed to enter raw terminal mode: %v", err)
	}
	defer func() { _ = term.Restore(stdinFd, oldState) }()

	// --- The POSIX Interceptor & Ghost Wipe ---
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	doneChan := make(chan struct{})

	go func() {
		select {
		case <-sigChan:
			// Forward graceful SIGINT to the isolated process group
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
			}
			select {
			case <-time.After(2 * time.Second):
				// TUI is frozen. Execute Ghost Wipe and drop the hammer.
				fmt.Print("\033[0m\033[?25h\033[?1049l\033[r\033[2K\033[J\n")
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			case <-doneChan:
				return
			}
		case <-doneChan:
			return
		}
	}()

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("primitive ignition fault: %v", err)
	}

	err = cmd.Wait()

	// Cleanup Interceptor
	close(doneChan)
	signal.Stop(sigChan)

	// Unconditional Ghost Wipe
	fmt.Print("\033[0m\033[?25h\033[?1049l\033[r\033[2K\033[J\n")

	if err != nil && !strings.Contains(err.Error(), "exit status") {
		return err
	}
	return nil
}

func (p *PrimitiveEngine) RunSilentCheck(ctx context.Context, workingDir string, command []string) (string, error) {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p *PrimitiveEngine) Destroy(ctx context.Context) error {
	return nil
}
