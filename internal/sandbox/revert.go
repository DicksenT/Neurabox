package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	WorktreePrefix = "nb-session-"
)

// IsGitRepo verifies the current working directory is an active Git repository
func IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

// CreateSessionWorktree creates a separate worktree for the agent session.
// The original directory remains untouched.
func CreateSessionWorktree(projectDir, sessionID string) (string, error) {
	// Create worktree in a temp directory
	worktreeDir := filepath.Join(os.TempDir(), WorktreePrefix+sessionID)

	// Clean up any existing worktree first
	_ = RemoveSessionWorktree(worktreeDir)

	// Create worktree from current HEAD
	cmd := exec.Command("git", "worktree", "add", "-f", worktreeDir, "HEAD")
	cmd.Dir = projectDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to create worktree: %s: %w", out, err)
	}

	// Copy untracked files to worktree (nb-policy.yaml, etc.)
	if err := copyUntrackedFiles(projectDir, worktreeDir); err != nil {
		_ = RemoveSessionWorktree(worktreeDir)
		return "", fmt.Errorf("failed to copy config files: %w", err)
	}

	fmt.Printf("🔀 Session worktree: %s\n", worktreeDir)
	return worktreeDir, nil
}

// RemoveSessionWorktree destroys the agent worktree
func RemoveSessionWorktree(worktreeDir string) error {
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		return nil
	}

	// Try git worktree remove first
	cmd := exec.Command("git", "worktree", "remove", "-f", worktreeDir)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fallback: try prune then remove
	_ = exec.Command("git", "worktree", "prune").Run()
	_ = exec.Command("git", "worktree", "remove", "-f", worktreeDir).Run()

	// Last resort: delete the directory
	return os.RemoveAll(worktreeDir)
}

// CleanupAllSessions removes all NeuraBox worktrees
// CleanupAllWorktrees finds and removes all NeuraBox worktrees
func CleanupAllWorktrees(projectDir string) error {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	var removed int
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			// Check if it's our worktree
			if strings.Contains(path, "nb-session-") || strings.Contains(path, "neurabox-") {
				fmt.Printf("🧹 Cleaning up: %s\n", path)
				if err := RemoveSessionWorktree(path); err == nil {
					removed++
				}
			}
		}
	}

	// Prune all worktrees
	_ = exec.Command("git", "worktree", "prune").Run()

	if removed == 0 {
		fmt.Println("ℹ No NeuraBox worktrees found.")
	} else {
		fmt.Printf(" Cleaned up %d worktree(s)\n", removed)
	}
	return nil
}

// copyUntrackedFiles copies config files that the agent needs
func copyUntrackedFiles(src, dst string) error {
	// Files that must be available in the worktree
	essentialFiles := []string{
		"nb-policy.yaml",
		".neurabox_failures.md",
		".claudeignore",
		".aiderignore",
		".cursorignore",
	}

	for _, f := range essentialFiles {
		srcPath := filepath.Join(src, f)
		if _, err := os.Stat(srcPath); err == nil {
			dstPath := filepath.Join(dst, f)
			if err := copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy %s: %w", f, err)
			}
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
