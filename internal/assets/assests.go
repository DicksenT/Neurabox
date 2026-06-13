package assets

import (
	_ "embed"
	"fmt"
	"runtime"
)

//go:embed neuragraph.exe
var Neuragraph []byte

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
    return nil, "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
}