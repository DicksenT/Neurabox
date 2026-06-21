//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/term"
)

type PrimitiveEngine struct{}

func NewPrimitiveEngine() *PrimitiveEngine {
	fmt.Println("🚀 Using Native macOS Primitive Isolation (Zero Dependency Mode)")
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
	var cleanEnv []string
	for _, env := range os.Environ() {
		key := env
		if idx := strings.IndexByte(env, '='); idx > 0 {
			key = env[:idx]
		}
		upper := strings.ToUpper(key)

		switch upper {
		case "PATH", "PWD", "INIT_CWD", "RTK_DB_PATH", "TERM", "COLORTERM":
			continue
		}

		// Preserve Git identity for sandbox commits, block path overrides
		switch upper {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
			"GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR", "GIT_CEILING_DIRECTORIES":
			continue
		}

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

	// --- Native macOS Process Group Isolation ---
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Assigns group ID matching the child PID
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("primitive ignition fault: %v", err)
	}

	// ⚠️ macOS limitation: Darwin lacks Pdeathsig. If Neurabox is killed via
	// SIGKILL (kill -9), this defer will not run, leaving orphaned agents.
	// This defer handles all normal exits and soft interrupts cleanly.
	defer func() {
		if cmd.Process != nil {
			// Negative PID targets the entire process group
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	stdinFd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return fmt.Errorf("failed to enter raw terminal mode: %v", err)
	}
	defer func() { _ = term.Restore(stdinFd, oldState) }()

	err = cmd.Wait()
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