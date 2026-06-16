package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"golang.org/x/term"
)

type Manager struct {
	Cli *client.Client
	ID  string
}

func DockerManager() *Manager {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil
	}
	defer cli.Close()
	_, err = cli.Ping(context.Background(), client.PingOptions{})
	if err != nil {
		fmt.Printf("❌ Docker is not responding. Is Docker Desktop running?\n")
		os.Exit(1)
	}
	fmt.Println("✅ Connected!")
	return &Manager{Cli: cli}
}

func (m *Manager) CreateSandbox(ctx context.Context, imageName string, mounts []mount.Mount) error {
	resp, err := m.Cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: imageName,
			Tty:   true,
			User:  "root",
			Env: []string{
				// Tell NPM to install global items into our persistent volume path
				"NPM_CONFIG_PREFIX=/root/.npm-global",
				// Append our new global path to the system execution PATH line
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/root/.npm-global/bin",
			},
		},
		HostConfig: &container.HostConfig{
			ExtraHosts:  []string{"host.docker.internal:host-gateway"},
			Mounts:      mounts,
			NetworkMode: "default",
		},
	})
	if err != nil {
		return err
	}
	m.ID = resp.ID
	_, startErr := m.Cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if startErr != nil {
		return startErr
	}
	return nil
}

func (m *Manager) RunInteractiveProxy(ctx context.Context, id string, command []string) error {
	// Get file descriptors for terminal tracking
	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())

	// 1. MEASURE YOUR ACTUAL SCREEN FIRST 📏
	width, height, err := term.GetSize(stdoutFd)
	if err != nil {
		width, height = 80, 24 // Fallback boundaries if reading fails
	}

	// 2. INJECT DIMENSIONS DIRECTLY INTO THE ENVIRONMENT AT BIRTH 🚀
	execConfig := client.ExecCreateOptions{
		Cmd:          command,
		AttachStdout: true,
		AttachStdin:  true,
		AttachStderr: true,
		TTY:          true,
		WorkingDir:   "/workspace",
		User:         "0:0",
		Env: []string{
			"TERM=xterm-256color",            // Enables full ANSI cursor navigation capabilities
			fmt.Sprintf("COLUMNS=%d", width), // Dictates matching width boundaries
			fmt.Sprintf("LINES=%d", height),  // Dictates matching height boundaries
		},
	}

	execId, err := m.Cli.ExecCreate(ctx, id, execConfig)
	if err != nil {
		return err
	}

	// 3. ATTACH TO YOUR PROCESS
	resp, err := m.Cli.ExecAttach(ctx, execId.ID, client.ExecAttachOptions{
		TTY: true,
	})
	if err != nil {
		return err // Fixed the silent nil return bug!
	}
	defer resp.Close()

	// Run an immediate backup resize sync to align Docker's backend PTY layer
	_, _ = m.Cli.ExecResize(ctx, execId.ID, client.ExecResizeOptions{
		Height: uint(height),
		Width:  uint(width),
	})

	// 4. PUT TERMINAL INTO RAW MODE RIGHT BEFORE STREAMING
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return err
	}
	defer func() {
		_ = term.Restore(stdinFd, oldState)
	}()

	// 5. START THE BACKGROUND MONITOR FOR LIVE DRAG/RESIZES
	monitorCtx, cancelMonitor := context.WithCancel(ctx)
	defer cancelMonitor()

	go func() {
		prevWidth, prevHeight := width, height
		for {
			select {
			case <-monitorCtx.Done():
				return
			default:
				if w, h, err := term.GetSize(stdoutFd); err == nil {
					if w != prevWidth || h != prevHeight {
						_, _ = m.Cli.ExecResize(monitorCtx, execId.ID, client.ExecResizeOptions{
							Height: uint(h),
							Width:  uint(w),
						})
						prevWidth, prevHeight = w, h
					}
				}
				time.Sleep(250 * time.Millisecond)
			}
		}
	}()

	// 6. BI-DIRECTIONAL IO.COPY STREAMS
	// 1. Kick off streaming background workers without channel closures
	go func() {
		_, _ = io.Copy(os.Stdout, resp.Reader)
	}()
	go func() {
		_, _ = io.Copy(resp.Conn, os.Stdin)
	}()

	// 2. BLOCK AND POLL DOCKER FOR THE TRUE PROCESS EXIT 🚀
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Query the container subsystem for the exact status of this specific Exec ID
			// Note: If using a shortened wrapper library, look for m.Cli.ExecInspect()
			inspect, err := m.Cli.ExecInspect(ctx, execId.ID, client.ExecInspectOptions{})
			if err != nil {
				return err
			}

			// If the process is no longer running, break out and clean up!
			if !inspect.Running {
				if inspect.ExitCode != 0 {
					return fmt.Errorf("agent exited with non-zero status code: %d", inspect.ExitCode)
				}
				return nil
			}

			// Sleep briefly to avoid pegging your laptop's CPU threads at 100%
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (m *Manager) ExecInSandbox(ctx context.Context, id string, command []string) error {
	execConfig := client.ExecCreateOptions{
		Cmd:          command,
		AttachStdout: true,
		AttachStdin:  false,
		AttachStderr: true,
		TTY:          false,
		WorkingDir:   "/workspace",
		User:         "0:0",
	}
	execID, err := m.Cli.ExecCreate(ctx, id, execConfig)
	if err != nil {
		return err
	}
	resp, err := m.Cli.ExecAttach(ctx, execID.ID, client.ExecAttachOptions{
		TTY: false,
	})
	if err != nil {
		return err
	}
	defer resp.Close()
	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, resp.Reader)
	if err != nil {
		return err
	}
	for {
		inspect, err := m.Cli.ExecInspect(ctx, execID.ID, client.ExecInspectOptions{})
		if err != nil {
			return err
		}
		if !inspect.Running {
			if inspect.ExitCode != 0 {
				return fmt.Errorf("Command failed with exit code %d\n", inspect.ExitCode)
			}
			break
		}

	}
	return nil
}
func (m *Manager) ExportChanges(shadowDir string, projectDir string, blockList []string) error {
	fmt.Println(blockList)
	sDir, _ := filepath.Abs(shadowDir)
	pDir, _ := filepath.Abs(projectDir)

	return filepath.Walk(sDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(sDir, path)
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(pDir, relPath)

		// 🛡️ THE "ALIVE" CHECK: Skip the binary and heavy folders
		name := info.Name()
		if info.IsDir() {
			if slices.Contains(blockList, name) {
				fmt.Printf("skipping dir")
				return filepath.SkipDir
			}
			// If MkdirAll fails, log it but don't crash the whole export
			_ = os.MkdirAll(targetPath, 0755)
			return nil
		}

		// 🚀 THE CRITICAL SKIP: Don't try to overwrite the running .exe
		if strings.HasSuffix(name, ".exe") || name == "audit.log" || slices.Contains(blockList, name) {
			fmt.Printf("Skipping locked file: %s\n", name)
			return nil
		}

		fmt.Printf("📝 Syncing: %s\n", relPath)

		// 🧪 WRAP IN ERROR HANDLER: If one file fails, keep going!
		err = m.copyFile(path, targetPath, info.Mode())
		if err != nil {
			fmt.Printf("⚠️  Skipped %s: %v\n", relPath, err)
		}

		return nil // Return nil so the Walk continues to the next file
	})
}

// Helper to keep the Walk function clean
func (m *Manager) copyFile(srcPath, dstPath string, mode os.FileMode) error {
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

func (m *Manager) DestroySandbox(ctx context.Context) error {
	// 1. Stop the container (5-second grace period)
	timeout := 5
	fmt.Printf("Stopping container %s...\n", m.ID[:12])
	if _, err := m.Cli.ContainerStop(ctx, m.ID, client.ContainerStopOptions{
		Timeout: &timeout,
	}); err != nil {
		// We don't return here because we still want to try removing it
		fmt.Printf("⚠️ Warning: Could not stop container: %v\n", err)
	}

	fmt.Printf("Removing container %s...\n", m.ID[:12])
	if _, err := m.Cli.ContainerRemove(ctx, m.ID, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}
