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
	"claude":   "CLAUDE.md",
	"gemini":   "GEMINI.md",
	"aider":    ".aider.md",
	"cursor":   ".cursorrules",
	"copilot":  ".github/copilot-instructions.md",
	"cline":    ".clinerules",
	"windsurf": ".windsurfrules",
	"default":  "AGENTS.md", // OpenAI standard, most new agents adopt this
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
	agentCmd []string, // We use this to detect the agent!
) {
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

	// 1. Detect which agent is being used
	cmdString := strings.ToLower(strings.Join(agentCmd, " "))
	targetFile := agentContextFiles["default"] // Defaults to AI_CONTEXT.md

	for key, filename := range agentContextFiles {
		if strings.Contains(cmdString, key) && key != "default" {
			targetFile = filename
			break
		}
	}

	fmt.Printf("Running neuragraph on projectDir: %s\n", projectDir)

	// 2. Pass the specific target file to the Python binary
	args := []string{projectDir, shadowDir, "--target-file", targetFile, "--debug"}

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

	if cacheDir != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("GRAPHIFY_OUT=%s", cacheDir))
	}

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		fmt.Println(" Context generation timed out — agent will start without project context.")
		return
	}
	if runErr != nil {
		fmt.Printf("  Context generation failed: %v\n", runErr)
		if stderrBuf.Len() > 0 {
			fmt.Printf("Details: %s\n", strings.TrimSpace(stderrBuf.String()))
		}
		return
	}

	// 3. Confirm only the targeted file landed in the shadow directory
	if info, err := os.Stat(filepath.Join(shadowDir, targetFile)); err == nil {
		tokens := int(info.Size()) / 4
		fmt.Printf(" Context injected: [%s (~%d tokens)]\n", targetFile, tokens)
	} else {
		fmt.Println(" Context generator ran but wrote no files — agent starts without context.")
	}
}
