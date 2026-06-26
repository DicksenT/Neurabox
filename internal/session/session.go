package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"bufio"
	"crypto/md5"

	"github.com/DicksenT/neurabox/internal/assets"
	"github.com/DicksenT/neurabox/internal/policy"
	"github.com/DicksenT/neurabox/internal/rtk"
	"github.com/DicksenT/neurabox/internal/sandbox"
	Types "github.com/DicksenT/neurabox/internal/types"
)

func RunProxySession(agentCmd []string, hardened bool) {
	cwd, _ := os.Getwd()
	policyPath := filepath.Join(cwd, "nb-policy.yaml")
	cfg, err := policy.Load(policyPath)
	if err != nil {
		log.Fatalf("Failed to load policy: %v", err)
	}

	projectDir, err := filepath.Abs(".")
	if err != nil {
		log.Fatal(err)
	}

	shadowDir, err := os.MkdirTemp("", "nb-proxy-*")
	if err != nil {
		log.Fatal(err)
	}

	assetDir, err := os.MkdirTemp("", "nb-assets-*")
	if err != nil {
		log.Fatalf("failed to allocate assets workspace: %v", err)
	}

	// --- RTK INITIALIZATION ---
	if err := rtk.Setup(assetDir, shadowDir); err != nil {
		log.Printf("Warning: RTK optimization layer failed to initialize: %v", err)
	}
	// --- DYNAMIC NEURAGRAPH CONTEXT BUILD ---
	var neuragraphPath string
	ngBinData, ngBinName, err := assets.GetNeuragraphBinary()
	if err != nil {
		log.Fatalf("failed to resolve embedded context engine: %v", err)
	}

	if ngBinData != nil {
		neuragraphPath = filepath.Join(assetDir, ngBinName)
		if err := os.WriteFile(neuragraphPath, ngBinData, 0755); err != nil {
			log.Fatalf("failed to initialize embedded context engine: %v", err)
		}

		// Crucial: Ensure Unix execution permissions are set for Linux and macOS targets
		if runtime.GOOS != "windows" {
			if err := os.Chmod(neuragraphPath, 0755); err != nil {
				log.Fatalf("failed to set context engine permissions: %v", err)
			}
		}
	}

	// BUG FIX : project-specific cache so multiple projects don't share a cache.
	projHash := fmt.Sprintf("%x", md5Hash(projectDir))[:8]
	cacheDir := filepath.Join(os.Getenv("APPDATA"), "neurabox", "neuragraph-cache", projHash)
	_ = os.MkdirAll(cacheDir, 0755)

	// BUG FIX : use bufio.Scanner so multi-word input like
	// "add login function" isn't truncated to "add" by fmt.Scan.
	fmt.Print("What is the purpose of this session? ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	purpose := strings.TrimSpace(scanner.Text())

	sandbox.ExportChanges(projectDir, shadowDir, cfg.Blocks)

	// BUG FIX 3: injectAgentBlindfolds must run AFTER ExportChanges so the
	// project's existing ignore files are present for the merge logic.
	if err := injectAgentBlindfolds(shadowDir, agentCmd); err != nil {
		log.Printf("Warning: Failed to apply token blindfolds: %v", err)
	}

	preSnap, snapErr := sandbox.SnapshotDir(shadowDir)
	if snapErr != nil {
		log.Printf("Warning: pre-session snapshot failed: %v — file tracking disabled", snapErr)
	}

	auditLogPath := filepath.Join(projectDir, "audit.log")
	WriteAgentContext(neuragraphPath, projectDir, shadowDir, auditLogPath, nil, cacheDir, agentCmd)
	
	// Map the initial shadow state to detect brand-new files later
	initialShadowFiles := make(map[string]bool)
	filepath.Walk(shadowDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(shadowDir, path)
			initialShadowFiles[rel] = true
		}
		return nil
	})
	runtime := sandbox.NewPrimitiveEngine()
	ctx := context.Background()

	fmt.Println("Launching isolated shadow workspace view")
	openCmd := exec.Command("cmd.exe", "/c", "code", shadowDir)
	_ = openCmd.Start()

	defer func() {
		_ = runtime.Destroy(ctx)
		os.RemoveAll(shadowDir)
		os.RemoveAll(assetDir) 
	}()
	for{
		if err := runtime.RunInteractive(ctx, shadowDir, agentCmd, assetDir, hardened); err != nil {
			fmt.Printf("Agent runtime session interrupted: %v\n", err)
		}
		agentLabel := strings.Join(agentCmd, " ")
		var changedFiles []string
		if preSnap != nil {
			if postSnap, err := sandbox.SnapshotDir(shadowDir); err == nil {
				changedFiles = sandbox.DiffSnapshots(preSnap, postSnap)
			}
		}
		// Guardrail checks.
		fmt.Println("\nInterception cycle complete. Running post-generation policy validations...")
		allPassed := true
		var failures []string
		for _, check := range cfg.Checks {
			fmt.Printf("Running guardrail: %s... ", check.CName)
			
			// output contains the exact stack trace, error logs, or stdout from the check
			output, checkErr := runtime.RunSilentCheck(ctx, shadowDir, []string{"sh", "-c", check.Command})

			if checkErr != nil && !strings.Contains(output, "Safe:") {
				fmt.Printf("FAILED\nReason: Policy violation detected.\n")
				
				// Clean up the output so it doesn't break markdown
				cleanOutput := strings.TrimSpace(output)
				if cleanOutput == "" {
					cleanOutput = checkErr.Error()
				}

				// Bundle the rule name, the command that ran, AND the exact terminal output
				richContext := fmt.Sprintf("**Rule:** %s\n  **Executed:** `%s`\n  **Terminal Trace:**\n  ```text\n  %s\n  ```", 
					check.CName, check.Command, cleanOutput)
				
				failures = append(failures, richContext)
				allPassed = false
			}
		}
		fmt.Printf("Running guardrail: ghost-overwrite-protection... ")
		collisionFound := false
		for _, relPath := range changedFiles {
			hostPath := filepath.Join(projectDir, relPath)
			
			// If the file was NOT in the shadow dir when we started...
			if !initialShadowFiles[relPath] {
				// ...but it ALREADY exists in the real host project...
				if _, err := os.Stat(hostPath); err == nil {
					// We caught a Ghost Overwrite!
					collisionMsg := fmt.Sprintf("GHOST COLLISION: You tried to create '%s', but this file already exists in the host environment and is hidden for security. You MUST use a different filename.", relPath)
					failures = append(failures, collisionMsg)
					collisionFound = true
					allPassed = false
				}
			}
		}
		
		if collisionFound {
			fmt.Printf("FAILED\nReason: Agent attempted to overwrite a hidden host file., please check or modify it\n")
		} else {
			fmt.Printf("Safe: No host collisions detected\n")
		}

		audit := Types.AuditEntry{
			ID:        "proxy-" + filepath.Base(shadowDir), // Unique session trace string
			TestPass:  allPassed,
			Purpose:   purpose, // Tracks exactly what command the user typed for the agent
			Agent:     agentLabel,
			Files:     changedFiles, // Tracks string slice of every single added/modified file
			BlockList: cfg.Blocks,
			Overridden: false,
		}
		fmt.Println("\n--- NEURABOX AUDIT GATE ---")
		if allPassed {
			fmt.Println("Guardrails: ALL PASSED")
			break
		}
		fmt.Println("\nNEURABOX GUARDRAIL VIOLATIONS DETECTED:")
		if promptYN("Would you like to re-call the agent to auto-correct these violations?") {
			// Inject the error context into the workspace so the agent reads it on retry
			err := injectFailureContext(shadowDir, failures)
			if err != nil {
				fmt.Printf("Warning: failed to inject error context file: %v\n", err)
			}
			
			fmt.Println("Spinning up agent retry loop with policy feedback...")
			continue // Restarts the loop, re-executing the agent command inside shadowDir
		}

		// 5. The Escape Hatch / Manual Override fallback
		if promptYN("Do you want to manually override and keep the code anyway?") {
			fmt.Println(" Manual override accepted. Proceeding to review stage...")
			audit.TestPass = false
			audit.Overridden = true
			break // Break out of the loop to allow export
		}

		// If they don't want to retry and don't want to override, discard the session
		fmt.Println("Export canceled, shadow workspace discarded.")
		sandbox.AuditLog(&audit)
		return
	}
	if promptYN("do you want to see the git Diff?") {
		cmd := exec.Command("git", "--no-pager", "diff", "--no-index", shadowDir, projectDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
	if promptYN("Confirm export to your real project?") {
		exported, _ := sandbox.ExportChangesTracked(shadowDir, projectDir, nil)
		fmt.Println("Project updated")
		printInstallReminder(exported)
		return
	} 
	fmt.Println("Export canceled, shadow workspace discarded")
		

}

// promptYN prints "[y/N]: " after the question and returns true only for "y"/"Y".
func promptYN(question string) bool {
	fmt.Printf("%s [y/N]: ", question)
	var answer string
	fmt.Scanln(&answer)
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}
func printInstallReminder(exported []string) {
	for _, f := range exported {
		switch filepath.Base(f) {
		case "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
			fmt.Println("\n package manifest changed — run `npm install` (or yarn/pnpm) to apply dependency changes.")
			return
		case "go.mod", "go.sum":
			fmt.Println("\n go.mod changed — run `go mod tidy` to apply dependency changes.")
			return
		case "requirements.txt", "Pipfile", "pyproject.toml":
			fmt.Println("\n Python dependencies changed — run `pip install -r requirements.txt` to apply changes.")
			return
		}
	}
	fmt.Println("for safety, file deleted by agent is not deleted in original source")
}

// md5Hash returns the MD5 digest of a string as a byte slice.
func md5Hash(s string) []byte {
	h := md5.New()
	h.Write([]byte(s))
	return h.Sum(nil)
}
func injectAgentBlindfolds(shadowDir string, agentCmd []string) error {
	neuraboxRules := `
# --- BEGIN NEURABOX TOKEN GUARD ---
*.png
*.jpg
*.jpeg
*.gif
*.svg
*.mp4
*.wav
*.mp3
*.pdf
*.zip
*.tar.gz
*.tgz
*.db
*.sqlite
*.sqlite3
*.local
.cache/
*.log
npm-debug.log*
yarn-debug.log*
pnpm-debug.log*
coverage/
.nyc_output/
htmlcov/
docs/_build/
site/
# --- END NEURABOX TOKEN GUARD ---`

	// Match the execution command to the correct configuration target
	cmdString := strings.ToLower(strings.Join(agentCmd, " "))
	var target string

	if strings.Contains(cmdString, "claude") {
		target = ".claudeignore"
	} else if strings.Contains(cmdString, "aider") {
		target = ".aiderignore"
	} else if strings.Contains(cmdString, "cursor") {
		target = ".cursorignore"
	} else if strings.Contains(cmdString, "copilot") {
		target = ".copilotignore"
	} else if strings.Contains(cmdString, "codeium") {
		target = ".codeiumignore"
	} else {
		// If they run a generic/custom script, write a universal fallback
		// (Many generic agents honor .ignore or .rgignore)
		target = ".ignore"
	}

	targetPath := filepath.Join(shadowDir, target)
	var finalContent string

	if _, err := os.Stat(targetPath); err == nil {
		existingBytes, err := os.ReadFile(targetPath)
		if err != nil {
			return err
		}
		cleanedContent := stripOldNeuraboxBlock(string(existingBytes))
		finalContent = cleanedContent + "\n" + strings.TrimSpace(neuraboxRules)
	} else {
		finalContent = "# Neurabox Token Guard\n" + strings.TrimSpace(neuraboxRules)
	}

	return os.WriteFile(targetPath, []byte(finalContent), 0644)
}

// Helper to sanitize files and remove old blocks on re-runs
func stripOldNeuraboxBlock(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")

	// Locate our distinct block tags
	startIdx := strings.Index(normalized, "# --- BEGIN NEURABOX TOKEN GUARD ---")
	endIdx := strings.Index(normalized, "# --- END NEURABOX TOKEN GUARD ---")

	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		// Slice out the block, keeping everything before and after it
		before := normalized[:startIdx]
		after := normalized[endIdx+len("# --- END NEURABOX TOKEN GUARD ---"):]
		return strings.TrimSpace(before + after)
	}

	return strings.TrimSpace(content)
}
func injectFailureContext(shadowDir string, failures []string) error {
	filePath := filepath.Join(shadowDir, ".neurabox_failures.md")
	
	var sb strings.Builder
	sb.WriteString("# ⚠️ CRITICAL: NEURABOX POLICY VIOLATION NOTICE\n\n")
	sb.WriteString("Your last execution failed one or more system guardrails or tests.\n")
	sb.WriteString("You MUST analyze the terminal traces below and correct your code in the next turn:\n\n")
	
	for i, failure := range failures {
		sb.WriteString(fmt.Sprintf("### Violation %d\n", i+1))
		sb.WriteString(failure)
		sb.WriteString("\n\n---\n\n") // Visual separator for multiple failures
	}
	
	sb.WriteString("Please fix the issues listed above, and remove this file once you comply.\n")

	return os.WriteFile(filePath, []byte(sb.String()), 0644)
}