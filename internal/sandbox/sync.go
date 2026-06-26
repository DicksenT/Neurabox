package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// heavyDirs are always skipped regardless of the policy blocklist.
// These are directories that are either generated, dependency caches, or
// version-control internals — copying them into the shadow workspace would
// be slow and pointless.
var heavyDirs = []string{
	"node_modules",
	".git",
	"dist",
	"build",
	".next",
	".nuxt",
	"vendor", // Go / PHP
	"__pycache__",
	".venv",
	"venv",
	".mypy_cache",
	".pytest_cache",
	"target", // Rust / Maven
	".gradle",
	".idea",
	".vscode",
}

// FileSnapshot records the mtime and size of every file in a directory tree.
// Used to detect which files actually changed after an agent session.
type FileSnapshot map[string]fileInfo

type fileInfo struct {
	Size    int64
	ModTime time.Time
}

// SnapshotDir walks dir and records mtime+size for every file.
func SnapshotDir(dir string) (FileSnapshot, error) {
	snap := make(FileSnapshot)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		snap[rel] = fileInfo{Size: info.Size(), ModTime: info.ModTime()}
		return nil
	})
	return snap, err
}

// DiffSnapshots compares a before and after snapshot and returns the relative
// paths of files that were added, modified, or deleted.
func DiffSnapshots(before, after FileSnapshot) []string {
	seen := make(map[string]bool)
	var changed []string

	for rel, afterInfo := range after {
		seen[rel] = true
		beforeInfo, existed := before[rel]
		if !existed {
			changed = append(changed, rel) // Pure relative tracking
		} else if afterInfo.Size != beforeInfo.Size || afterInfo.ModTime.After(beforeInfo.ModTime) {
			changed = append(changed, rel) // Pure relative tracking
		}
	}
	for rel := range before {
		if !seen[rel] {
			changed = append(changed, rel) // Pure relative tracking
		}
	}
	return changed
}

// ExportChanges copies every file from sourceDir to targetDir, skipping:
//   - directories in blockList (from nb-policy.yaml)
//   - heavyDirs (node_modules, .git, dist, etc.)
//   - .exe files and audit.log (locked/irrelevant)
//   - files that are identical to the target (same size + mtime) — avoids
//     redundant I/O when syncing a large workspace back to the project
//
// It also syncs deletions: files that exist in targetDir but not in sourceDir
// are removed, so the export is a true mirror rather than just an overlay.
//
// Returns the list of relative paths that were actually written or deleted.
func ExportChanges(sourceDir string, targetDir string, blockList []string) error {
	_, err := exportChanges(sourceDir, targetDir, blockList)
	return err
}

// ExportChangesTracked is like ExportChanges but also returns the list of
// relative paths that were written or deleted. Use this when you need to
// show the user a summary of what changed.
func ExportChangesTracked(sourceDir string, targetDir string, blockList []string) ([]string, error) {
	return exportChanges(sourceDir, targetDir, blockList)
}

func exportChanges(sourceDir string, targetDir string, blockList []string) ([]string, error) {
	sDir, _ := filepath.Abs(sourceDir)
	pDir, _ := filepath.Abs(targetDir)

	var changed []string

	// --- Phase 1: Sync deletions ---
	// Walk the TARGET and remove anything that no longer exists in the source.
	// This ensures deleted files don't silently persist in the real project.
	/*err := filepath.WalkDir(pDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // target may not exist yet; that's fine
		}
		relPath, _ := filepath.Rel(pDir, path)
		if relPath == "." {
			return nil
		}

		// Never try to delete heavy dirs from the target — they were never
		// copied there in the first place.
		name := d.Name()
		if d.IsDir() && (slices.Contains(heavyDirs, name) || slices.Contains(blockList, name)) {
			return filepath.SkipDir
		}

		srcPath := filepath.Join(sDir, relPath)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			fmt.Printf("🗑️  Deleting: %s\n", relPath)
			if removeErr := os.RemoveAll(path); removeErr != nil {
				fmt.Printf("⚠️  Could not delete %s: %v\n", relPath, removeErr)
			} else {
				changed = append(changed, relPath+" (deleted)")
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("deletion sync failed: %v", err)
	}
	*/
	// --- Phase 2: Copy new/changed files from source to target ---
	err := filepath.Walk(sDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(sDir, path)
		if relPath == "." {
			return nil
		}

		name := info.Name()

		// Skip heavy and blocked directories entirely.
		if info.IsDir() {
			if slices.Contains(blockList, name) {
				fmt.Printf(" Skipping dir: %s\n", relPath)
				return filepath.SkipDir
			}
			if slices.Contains(heavyDirs, name) {
				targetPath := filepath.Join(pDir, relPath)
				if _, statErr := os.Stat(targetPath); os.IsNotExist(statErr) {
					fmt.Printf("Linking heavy dir: %s\n", relPath)
					_ = createSymlink(path, targetPath)
				}
			}
			_ = os.MkdirAll(filepath.Join(pDir, relPath), 0755)
			return nil
		}

		// Skip locked / irrelevant file types.
		if strings.HasSuffix(name, ".exe") || name == "audit.log" || slices.Contains(blockList, name) ||
			name == ".claudeignore" || name == ".aiderignore" || name == ".cursorignore" ||
			name == ".copilotignore" || name == ".codeiumignore" || name == ".ignore" || name == "nb-graph" || name == "AI_CONTEXT.md" || name == "graph.json" {
			return nil // Spared! This stays inside the shadow directory and is never synced back to the host.
		}
		targetPath := filepath.Join(pDir, relPath)

		// Skip the copy if the target already has the same content.
		// We use size+mtime as a fast proxy — good enough for agent outputs.
		if targetInfo, err := os.Stat(targetPath); err == nil {
			if targetInfo.Size() == info.Size() && !info.ModTime().After(targetInfo.ModTime()) {
				return nil // identical — no I/O needed
			}
		}

		fmt.Printf("📝 Syncing: %s\n", relPath)
		if copyErr := CopyFile(path, targetPath, info.Mode()); copyErr != nil {
			fmt.Printf("⚠️  Skipped %s: %v\n", relPath, copyErr)
		} else {
			changed = append(changed, relPath)
		}

		return nil
	})

	return changed, err
}

// copyFile copies srcPath to dstPath preserving the given mode.
func CopyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// createSymlink attempts to create a directory symlink.
// On Windows, it falls back to a Junction point if standard symlinks fail.
func createSymlink(src string, dst string) error {
	err := os.Symlink(src, dst)
	
	// If Symlink fails (common on Windows without Developer Mode), use mklink /J
	if err != nil {
		isContainPriv := strings.Contains(strings.ToLower(err.Error()), "privilege")

		var pathError *os.PathError
		isPathError := errors.As(err, &pathError)
		if isContainPriv || isPathError {
			cmd := exec.Command("cmd", "/c", "mklink", "/J", dst, src)
			return cmd.Run()
		}
	}
	return err
}