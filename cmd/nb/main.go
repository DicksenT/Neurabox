package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DicksenT/neurabox/internal/policy"
	"github.com/DicksenT/neurabox/internal/sandbox"
	"github.com/moby/moby/api/types/mount"
)

func main() {
	fmt.Println("Starting neurabox")
	cfg, err := policy.Load("nb-policy.yaml")
	if(err!=nil){
		log.Fatalf("Failed to load policy: %v", err)
	}
	projectDir, err := os.Getwd()
	if(err!=nil){
		log.Fatal(err)
	}
	//create shadow dir
	shadowDir, err := os.MkdirTemp("", "neurabox-*")
	if(err!=nil){
		log.Fatal(err)
	}
	defer os.RemoveAll(shadowDir)
	fmt.Println("successfully create temp dir")
	mgr,_ := sandbox.NewManager()
	mgr.ExportChanges(projectDir, shadowDir)
	
	var dockerMounts []mount.Mount
	for _,m := range cfg.Mounts{
		relPath := m.Source
		sourceInShadow := filepath.Join(shadowDir, relPath)
		absSource,_:= filepath.Abs(sourceInShadow)
		readonly :=(m.Mode == "ro")
		
		dockerMounts = append(dockerMounts, mount.Mount{
			Type: mount.TypeBind,
			Source: absSource,
			Target: m.Target,
			ReadOnly: readonly,
		})
	}
	ctx := context.Background()

	sandboxError := mgr.CreateSandbox(ctx, cfg.Image, dockerMounts)
	if(sandboxError!=nil){
		log.Fatalf("failed to start sandbox: %v", sandboxError)
	}

	fmt.Printf("Sandbox started!, id: %s\n", mgr.ID)
	fmt.Println("Code now isolated, it cannot see your system or the internet")

	// ... after initial sync ...
	prompt := "create function to say hello"
	fmt.Println(" AI is thinking...")
	response, _ := mgr.AskAI(ctx, prompt, shadowDir)

	// tell result
	fmt.Printf("AI Suggestion: %s\n", response)


	// ... run checks ...
	allPassed := true
	for _,check := range cfg.Checks{
		fmt.Printf("Running guardrail: %s...\n", check.CName)

		err := mgr.ExecInSandbox(ctx, mgr.ID, []string{"sh", "-c", check.Command})
		if(err!=nil){
			fmt.Printf("Failed, %s, blocking code export\n", check.CName)
			allPassed = false
			break
		}
	}
	if allPassed{
		fmt.Println("\n--- NEURABOX AUDIT GATE ---")
		fmt.Printf("Guardrails: ALL PASSED\n")
		cmd := exec.Command("git", 
			"-c", "core.safecrlf=false", 
			"--no-pager", 
			"diff", 
			"--no-index", 
			"--stat",           
			"--compact-summary",  
			shadowDir, 
			projectDir,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
		fmt.Printf("Confirm export to your real project? [y/N]: ")
		
		var confirm string
		fmt.Scanln(&confirm)

		if(strings.ToLower(confirm) == "y"){
			//mgr.ExportChanges(shadowDir, projectDir)
			fmt.Println("Project updated")
		}else{
			fmt.Println("Export canceled, shadow workspace discarded")
		}
	}
}