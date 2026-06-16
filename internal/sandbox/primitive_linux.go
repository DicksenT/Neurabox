//go:build linux

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
	fmt.Println("🚀 Using Native Linux Primitive Isolation (Zero Dependency Mode)")
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

		// Strip path-sensitive variables to rebuild securely below
		switch upper {
		case "PATH", "PWD", "INIT_CWD", "RTK_DB_PATH", "TERM", "COLORTERM":
			continue
		}

		// OBLITERATE host-context, IDE sockets, and AI Memory Trackers
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

	// Re-add controlled variables. Note the POSIX colon (:) separator for PATH.
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

	// Capture terminal state and transition safely to raw terminal mode
	stdinFd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return fmt.Errorf("failed to enter raw terminal mode: %v", err)
	}
	defer func() { _ = term.Restore(stdinFd, oldState) }()

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("primitive ignition fault: %v", err)
	}

	err = cmd.Wait()
	if err != nil && !strings.Contains(err.Error(), "exit status") {
		return err
	}
	return nil
}

func (p *PrimitiveEngine) RunSilentCheck(ctx context.Context, workingDir string, command []string) (string, error) {
	// Native POSIX handles 'sh -c' directly. No translation required.
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p *PrimitiveEngine) Destroy(ctx context.Context) error {
	return nil
}
