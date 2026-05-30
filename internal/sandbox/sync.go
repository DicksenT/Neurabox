package sandbox

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func ExportChanges(sourceDir string, targetDir string, blockList []string) error {
	fmt.Println(blockList)
	sDir, _ := filepath.Abs(sourceDir)
	pDir, _ := filepath.Abs(targetDir)

	/*
	//ensure
	err := filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get the relative path from the destination root
		relPath, err := filepath.Rel(targetDir, path)
		if err != nil {
			return err
		}

		// Skip checking the root folder itself
		if relPath == "." {
			return nil
		}

		// Check if this file/folder exists in the target folder
		correspondingSrcPath := filepath.Join(sourceDir, relPath)
		if _, err := os.Stat(correspondingSrcPath); os.IsNotExist(err) {
			// 🚨 File exists in original code, but does NOT exist in shadow workspace!
			// The agent deliberately deleted it. We must delete it from the destination.
			fmt.Printf("  Syncing Deletion: Removing %s\n", relPath)
			
			// RemoveAll handles both individual files and entire removed directories
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			
			// If we deleted a directory, tell WalkDir to skip tracking its internal files
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sync file deletions: %v", err)
	}
*/
	return filepath.Walk(sDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(sDir, path)
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(pDir, relPath)

		//  THE "ALIVE" CHECK: Skip the binary and heavy folders
		name := info.Name()
		if info.IsDir() {
			if slices.Contains(blockList, name) {
				fmt.Printf("skipping dir")
				return filepath.SkipDir
			}
			if name == ".claude" || name == ".aider" || name == ".gemini" || name == ".vscode" {
		return filepath.SkipDir
	}
			// If MkdirAll fails, log it but don't crash the whole export
			_ = os.MkdirAll(targetPath, 0755)
			return nil
		}

		//  THE CRITICAL SKIP: Don't try to overwrite the running .exe
		if strings.HasSuffix(name, ".exe") || name == "audit.log" || slices.Contains(blockList, name) {
			fmt.Printf("Skipping locked file: %s\n", name)
			return nil
		}

		fmt.Printf("Syncing: %s\n", relPath)

		// 🧪 WRAP IN ERROR HANDLER: If one file fails, keep going!
		err = copyFile(path, targetPath, info.Mode())
		if err != nil {
			fmt.Printf("Skipped %s: %v\n", relPath, err)
		}

		return nil // Return nil so the Walk continues to the next file
	})
}

// Helper to keep the Walk function clean
func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	// Create/Truncate destination
	dst, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}