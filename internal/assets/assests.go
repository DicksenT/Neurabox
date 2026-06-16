package assets

import (
	_ "embed"
	"fmt"
	"runtime"
)

// --- Neuragraph Binary Embeds ---

//go:embed neuragraph-windows.exe
var neuragraphWindowsExe []byte

//go:embed neuragraph-linux
var neuragraphLinuxAmd64 []byte

//go:embed neuragraph-macos
var neuragraphMacos []byte

// --- RTK Binary Embeds ---

//go:embed rtk-x86_64-pc-windows-msvc.exe
var rtkWindowsExe []byte

//go:embed rtk-x86_64-unknown-linux-musl
var rtkLinuxAmd64 []byte

//go:embed rtk-aarch64-unknown-linux-gnu
var rtkLinuxArm64 []byte

//go:embed rtk-x86_64-apple-darwin
var rtkDarwinAmd64 []byte

//go:embed rtk-aarch64-apple-darwin
var rtkDarwinArm64 []byte

// GetRTKBinary returns the correct RTK binary data and filename for the current platform.
func GetRTKBinary() ([]byte, string, error) {
	switch runtime.GOOS {
	case "windows":
		return rtkWindowsExe, "rtk.exe", nil
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return rtkLinuxAmd64, "rtk", nil
		case "arm64":
			return rtkLinuxArm64, "rtk", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return rtkDarwinAmd64, "rtk", nil
		case "arm64":
			return rtkDarwinArm64, "rtk", nil
		}
	}
	return nil, "", fmt.Errorf("unsupported platform for RTK: %s/%s", runtime.GOOS, runtime.GOARCH)
}

// GetNeuragraphBinary returns the correct Neuragraph context parser data and filename for the current platform.
func GetNeuragraphBinary() ([]byte, string, error) {
	switch runtime.GOOS {
	case "windows":
		return neuragraphWindowsExe, "neuragraph_context.exe", nil
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return neuragraphLinuxAmd64, "neuragraph_context", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64", "arm64":
			// Pointing both to the universal/native Mac binary built by your GitHub action
			return neuragraphMacos, "neuragraph_context", nil
		}
	}
	return nil, "", fmt.Errorf("unsupported platform for Neuragraph: %s/%s", runtime.GOOS, runtime.GOARCH)
}
