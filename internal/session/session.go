package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DicksenT/neurabox/internal/policy"
	"github.com/DicksenT/neurabox/internal/sandbox"
	Types "github.com/DicksenT/neurabox/internal/types"
	"github.com/moby/moby/api/types/mount"
)

func RunSession(prompt string){
	cwd, _ := os.Getwd()
	policyPath:= filepath.Join(cwd, "nb-policy.yaml")
	cfg, err := policy.Load(policyPath)
	if(err!=nil){
		log.Fatalf("Failed to load policy: %v", err)
	}

	projectDir, err := filepath.Abs(".")
	if(err!=nil){
		log.Fatal(err)
	}
	//create shadow dir
	shadowDir, err := os.MkdirTemp("", "neurabox-*")
	if(err!=nil){
		log.Fatal(err)
	}
	defer os.RemoveAll(shadowDir)
	mgr := sandbox.NewManager()
	ctx := context.Background()
	defer func() {
        fmt.Printf("Cleaning up sandbox %s...\n", mgr.ID[:12])
        mgr.DestroySandbox(ctx) 
        os.RemoveAll(shadowDir) // Delete the temp folder too
    }()
	audit := Types.AuditEntry{
		ID: mgr.ID,
		TestPass: false,
		Approved: false,
	}
	fmt.Printf("printing block list:")
	fmt.Println(cfg.Blocks)
	mgr.ExportChanges(projectDir, shadowDir, cfg.Blocks)

	for i := 0; i < 5; i++ {
    if _, err := os.Stat(filepath.Join(shadowDir, "src")); err == nil {
        break 
    }
    time.Sleep(100 * time.Millisecond)
}

	//mount
	var dockerMounts []mount.Mount
	for _,m := range cfg.Mounts{
		var sourcePath string
		if m.Mode == "rw" {
        sourcePath = filepath.Join(shadowDir, m.Source)
    } else {
        sourcePath = filepath.Join(projectDir, m.Source) // Mount from real project
    }

    absSource, _ := filepath.Abs(sourcePath)
	if _, err := os.Stat(absSource); os.IsNotExist(err) {
        fmt.Printf("Warning: Mount source %s does not exist. Skipping.\n", absSource)
        continue 
    }
		dockerMounts = append(dockerMounts, mount.Mount{
			Type: mount.TypeBind,
			Source: absSource,
			Target: m.Target,
			ReadOnly: m.Mode == "ro",
		})
	}

	
	sandboxError := mgr.CreateSandbox(ctx, cfg.Image, dockerMounts)
	if(sandboxError!=nil){
		log.Fatalf("failed to start sandbox: %v", sandboxError)
	}

	fmt.Printf("Sandbox started!, id: %s\n", mgr.ID)
	fmt.Println("Code now isolated, it cannot see your system or the internet")

	// ... after initial sync ...
	audit.Prompt = prompt
	fmt.Println(" AI is thinking...")
	response, model,_ := mgr.AskAI(ctx, prompt, shadowDir)
	audit.Agent = model

	// tell result
	fmt.Printf("AI Suggestion: %s\n", response)
	files, err := mgr.ApplyAI(shadowDir, response)
	audit.Files = files

	// ... run checks ...
	allPassed := true
	for _,check := range cfg.Checks{
		fmt.Printf("Running guardrail: %s...\n", check.CName)
		err := mgr.ExecInSandbox(ctx, mgr.ID, []string{"sh", "-c", check.Command})
		if(err!=nil){
			fmt.Printf("Failed, %s, blocking code export\n", check.CName)
			fmt.Printf("error: %s", err)
			allPassed = false
			break
		}
	}
	if allPassed{
		audit.TestPass = true
		fmt.Println("\n--- NEURABOX AUDIT GATE ---")
		fmt.Printf("Guardrails: ALL PASSED\n")
		fmt.Printf("do you want to see the git Diff? [y/N]: ")
		var show string
		fmt.Scanln(&show)
		if(strings.ToLower(show) == "y"){
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
		}
		fmt.Printf("Confirm export to your real project? [y/N]: ")
		
		var confirm string
		fmt.Scanln(&confirm)

		if(strings.ToLower(confirm) == "y"){
			audit.Approved = true
			mgr.ExportChanges(shadowDir, projectDir, nil)
			fmt.Println("Project updated")
		}else{
			fmt.Println("Export canceled, shadow workspace discarded")
		}
	}
	mgr.AuditLog(&audit)
} 