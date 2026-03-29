package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type Manager struct{
	Cli *client.Client
	ID string
}

func NewManager()(*Manager, error){
	os.Setenv("OLLAMA_HOST", "http://127.0.0.1:11434")
	cli, err := client.New(client.FromEnv)
	if(err!=nil){
		return nil, err
	}
	return &Manager{Cli:cli}, err
}

func (m *Manager) CreateSandbox(ctx context.Context, imageName string, mounts []mount.Mount) error{
	resp, err := m.Cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: imageName,
			Tty: true,
			NetworkDisabled: true,
		},
		HostConfig: &container.HostConfig{
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
			Mounts: mounts,
		},
	})
	if(err!=nil){
		return err
	}
	m.ID = resp.ID
	_, startErr := m.Cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if(startErr!=nil){
		return startErr
	}
	return nil
}

func (m *Manager) ExecInSandbox(ctx context.Context, id string, command []string) error{
	execConfig := client.ExecCreateOptions{
		Cmd:	command,
		AttachStdout: true,
		AttachStdin: true,
		TTY: false,
	}
	commandString := fmt.Sprintf("%v", command)
	m.AuditLog(commandString)
	execID, err := m.Cli.ExecCreate(ctx, id, execConfig)
	if(err!=nil){
		return err
	}
	resp, err := m.Cli.ExecAttach(ctx, execID.ID, client.ExecAttachOptions{
		TTY: false,
	})	
	if(err!=nil){
		return err
	}
	defer resp.Close()
	_,err = stdcopy.StdCopy(os.Stdout, os.Stderr, resp.Reader)
	if(err!=nil){
		return err
	}
	inspect, err := m.Cli.ExecInspect(ctx, execID.ID, client.ExecInspectOptions{})
	if(err!=nil){
		return err
	}
	if inspect.ExitCode !=0{
		return fmt.Errorf("Command failed with exit code %d", inspect.ExitCode)
	}
	return nil
}

func (m *Manager) ExportChanges(shadowDir string, projectDir string) error {

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// /E = copies directories and subdirectories, including empty ones.
		// /Y = suppresses prompting to confirm you want to overwrite.
		cmd = exec.Command("xcopy", shadowDir, projectDir, "/E", "/Y", "/I")
	} else {
		cmd = exec.Command("cp", "-r", shadowDir+"/.", projectDir)
	}

	return cmd.Run()
}