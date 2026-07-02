package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const StashMessage = "neurabox_snapshot_pre_session"

// IsGitRepo verifies the current working directory is an active Git repository
func IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

// SaveSnapshot stages and stashes everything (including untracked files) before the agent ignites
func SaveSnapshot() error {
	if !IsGitRepo() {
		return errors.New("not a git repository; skipping safety snapshot")
	}

	// 1. Stage all current changes so stash captures untracked files correctly
	if err := exec.Command("git", "add", "-A").Run(); err != nil {
		return fmt.Errorf("failed to stage current workspace files: %w", err)
	}

	// 2. Push changes into a distinct NeuraBox safety stash
	cmd := exec.Command("git", "stash", "push", "-u", "-m", StashMessage)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create pre-session safety stash: %w", err)
	}

	return nil
}

// CleanAndRestore Workspace forcefully deletes agent work and pops the original developer state back
func CleanAndRestore() error {
	if !IsGitRepo() {
		return errors.New("not a git repository")
	}

	// 1. Hard reset to obliterate agent edits on tracked files
	if err := exec.Command("git", "reset", "--hard", "HEAD").Run(); err != nil {
		return fmt.Errorf("failed to hard reset workspace: %w", err)
	}

	// 2. Clean -fd to forcefully purge any new directories/files the agent created
	if err := exec.Command("git", "clean", "-fd").Run(); err != nil {
		return fmt.Errorf("failed to purge untracked agent files: %w", err)
	}

	// 3. Find our specific NeuraBox stash index to avoid popping the user's personal stashes
	stashRef, err := findNeuraBoxStashRef()
	if err != nil {
		return fmt.Errorf("could not find restore point: %w", err)
	}

	// 4. Pop the snapshot back to restore the exact pre-session uncommitted state
	if err := exec.Command("git", "stash", "pop", stashRef).Run(); err != nil {
		return fmt.Errorf("failed to pop safety snapshot back into workspace: %w", err)
	}

	return nil
}

// findNeuraBoxStashRef parses 'git stash list' to find the exact matching identifier (e.g., stash@{0})
func findNeuraBoxStashRef() (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("git", "stash", "list")
	cmd.Stdout = &out
	
	if err := cmd.Run(); err != nil {
		return "", err
	}

	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		if strings.Contains(line, StashMessage) {
			// Line looks like: stash@{0}: On main: neurabox_snapshot_pre_session
			parts := strings.Split(line, ":")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0]), nil
			}
		}
	}

	return "", errors.New("no active neurabox snapshot found in git history")
}