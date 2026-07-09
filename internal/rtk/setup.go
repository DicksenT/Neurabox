package rtk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/DicksenT/neurabox/internal/assets"
)

func Setup(assetDir, shadowDir string) error {
	// 1. Resolve embedded assets cleanly
	rtkBinData, rtkBinName, err := assets.GetRTKBinary()
	if err != nil {
		return fmt.Errorf("failed to read embedded RTK binary: %w", err)
	}
	if rtkBinData == nil {
		return nil // Graceful degradation if assets are stripped
	}

	// 2. Write master administrative binary (0755 handles executable bits directly on Unix)
	rtkPath := filepath.Join(assetDir, rtkBinName)
	if err := os.WriteFile(rtkPath, rtkBinData, 0755); err != nil {
		return fmt.Errorf("failed to write master RTK binary: %w", err)
	}

	// 3. Provision Multi-Call Proxies
	interceptedTools := []string{"git", "npm", "npx"}
	for _, tool := range interceptedTools {
		if _, err := exec.LookPath(tool); err != nil {
			continue // Skip if host environment doesn't possess the underlying tool
		}

		filename := tool
		if runtime.GOOS == "windows" {
			filename += ".exe"
		}
		shimPath := filepath.Join(assetDir, filename)

		if err := os.WriteFile(shimPath, rtkBinData, 0755); err != nil {
			fmt.Printf("Warning: failed to clone proxy binary for %s: %v\n", tool, err)
			continue
		}
	}

	// 4. Build Metrics DB Path Structure
	// We handle the database initialization natively here, bypassing the need for an external 'rtk init' command.
	var dbPath string
	if runtime.GOOS == "windows" {
		dbPath = filepath.Join(os.Getenv("APPDATA"), "neurabox", "rtk_metrics.db")
	} else {
		dbPath = filepath.Join(os.Getenv("HOME"), ".config", "neurabox", "rtk_metrics.db")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err == nil {
		// Set globally for host, but remember to append to execution primitives!
		os.Setenv("RTK_DB_PATH", dbPath)
	}

	return nil
}
