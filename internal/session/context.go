package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// agentContextFiles maps agent name patterns to the filename they auto-read
// on startup. Written by graphify_ast.py directly into shadowDir.
var agentContextFiles = map[string]string{
	"claude":  "CLAUDE.md",
	"gemini":  "GEMINI.md",
	"aider":   ".aider.md",
	"cursor":  ".cursorrules",
	"default": "AI_CONTEXT.md",
}

// WriteAgentContext runs neuragraph.exe (PyInstaller-packaged graphify_ast.py)
// which writes CLAUDE.md, GEMINI.md, .aider.md, and AI_CONTEXT.md directly
// into shadowDir. No stdout capture — the binary handles file writing itself.
//
// neuragraphBinPath — path to embedded neuragraph.exe
// projectDir        — real project root (graph source of truth)
// shadowDir         — agent workspace (context files written here)
// auditLogPath      — absolute path to neurabox audit.log
// changedFiles      — from DiffSnapshots (nil on first session)
// cacheDir          — persistent project-specific cache (GRAPHIFY_OUT)
// agentCmd          — used only to report which file was expected
func WriteAgentContext(
	neuragraphBinPath string,
	projectDir string,
	shadowDir string,
	auditLogPath string,
	changedFiles []string,
	cacheDir string,
	agentCmd []string,
) {
	// Write changed files to a temp file for the Python script to read.
	var changedFilePath string
	if len(changedFiles) > 0 {
		tf, err := os.CreateTemp("", "nb-changed-*.txt")
		if err == nil {
			_, _ = tf.WriteString(strings.Join(changedFiles, "\n"))
			_ = tf.Close()
			changedFilePath = tf.Name()
			defer os.Remove(changedFilePath)
		}
	}

	// graphify_ast.py CLI: <project_dir> <shadow_dir> [--audit-log x] [--changed x]
	// Note: no --cache-dir flag — cacheDir is passed as GRAPHIFY_OUT env var.
	fmt.Printf("Running neuragraph on projectDir: %s\n", projectDir)
	args := []string{projectDir, shadowDir, "--debug"}
	if auditLogPath != "" {
		args = append(args, "--audit-log", auditLogPath)
	}
	if changedFilePath != "" {
		args = append(args, "--changed", changedFilePath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, neuragraphBinPath, args...)
	cmd.Dir = projectDir

	// BUG FIX 1: cacheDir goes as GRAPHIFY_OUT env var, not a CLI flag.
	// graphify_ast.py reads os.environ["GRAPHIFY_OUT"] to locate its cache.
	if cacheDir != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("GRAPHIFY_OUT=%s", cacheDir))
	}

	// Capture stderr for diagnostics only. stdout is not used — the binary
	// writes files directly into shadowDir (BUG FIX 2: no stdout capture).
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		fmt.Println("⚠️  Context generation timed out — agent will start without project context.")
		return
	}
	if runErr != nil {
		fmt.Printf("⚠️  Context generation failed: %v\n", runErr)
		if stderrBuf.Len() > 0 {
			fmt.Printf("Details: %s\n", strings.TrimSpace(stderrBuf.String()))
		}
		return
	}

	// Confirm which files actually landed in shadowDir.
	var written []string
	for _, name := range []string{"CLAUDE.md", "GEMINI.md", ".aider.md", "AI_CONTEXT.md"} {
		if info, err := os.Stat(filepath.Join(shadowDir, name)); err == nil {
			written = append(written, fmt.Sprintf("%s (~%d tokens)", name, int(info.Size())/4))
		}
	}
	if len(written) > 0 {
		fmt.Printf("📖 Context injected: [%s]\n", strings.Join(written, ", "))
	} else {
		fmt.Println("⚠️  Context generator ran but wrote no files — agent starts without context.")
	}
}
