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
	"github.com/DicksenT/neurabox/internal/sandbox"
	Types "github.com/DicksenT/neurabox/internal/types"
	"github.com/moby/moby/api/types/mount"
)

func RunSession(prompt string) {
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

	shadowDir, err := os.MkdirTemp("", "neurabox-*")
	if err != nil {
		log.Fatal(err)
	}
	mgr := sandbox.DockerManager()
	ctx := context.Background()
	defer func() {
		fmt.Printf("Cleaning up sandbox %s...\n", mgr.ID[:12])
		mgr.DestroySandbox(ctx)
		os.RemoveAll(shadowDir)
	}()
	mgr.ExportChanges(projectDir, shadowDir, cfg.Blocks)

	var dockerMounts []mount.Mount
	for _, m := range cfg.Mounts {
		var sourcePath string
		if m.Mode == "rw" {
			sourcePath = filepath.Join(shadowDir, m.Source)
		} else {
			sourcePath = filepath.Join(projectDir, m.Source)
		}
		absSource, _ := filepath.Abs(sourcePath)
		if _, err := os.Stat(absSource); os.IsNotExist(err) {
			fmt.Printf("Warning: Mount source %s does not exist. Skipping.\n", absSource)
			continue
		}
		dockerMounts = append(dockerMounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   absSource,
			Target:   m.Target,
			ReadOnly: m.Mode == "ro",
		})
	}

	if err := mgr.CreateSandbox(ctx, cfg.Image, dockerMounts); err != nil {
		log.Fatalf("failed to start sandbox: %v", err)
	}
	fmt.Printf("Sandbox started!, id: %s\n", mgr.ID)
	fmt.Println("Code now isolated, it cannot see your system or the internet")

	fmt.Println("AI is thinking...")
	response, model, _ := mgr.AskAI(ctx, prompt, shadowDir)
	fmt.Printf("AI Suggestion: %s\n", response)
	files, err := mgr.ApplyAI(shadowDir, response)

	audit := Types.AuditEntry{
		ID:       mgr.ID,
		TestPass: false,
		Approved: false,
		Purpose: "",
		Agent:    model,
		Files:    files,
	}

	allPassed := true
	for _, check := range cfg.Checks {
		fmt.Printf("Running guardrail: %s...\n", check.CName)
		if err := mgr.ExecInSandbox(ctx, mgr.ID, []string{"sh", "-c", check.Command}); err != nil {
			fmt.Printf("Failed, %s, blocking code export\nerror: %s\n", check.CName, err)
			allPassed = false
			break
		}
	}

	if allPassed {
		audit.TestPass = true
		fmt.Println("\n--- NEURABOX AUDIT GATE ---")
		fmt.Println("Guardrails: ALL PASSED")
		if promptYN("do you want to see the git Diff?") {
			cmd := exec.Command("git", "--no-pager", "diff", "--no-index", shadowDir, projectDir)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		}
		if promptYN("Confirm export to your real project?") {
			audit.Approved = true
			mgr.ExportChanges(shadowDir, projectDir, nil)
			fmt.Println("Project updated")
		} else {
			fmt.Println("Export canceled, shadow workspace discarded")
		}
	}
	sandbox.AuditLog(&audit)
}

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

		// Set up rtk locally (non‑interactive, inside sandbox only)
	rtkBinData, rtkBinName,_ := assets.GetRTKBinary()
    if rtkBinData != nil {
        rtkPath := filepath.Join(assetDir, rtkBinName)
        if err := os.WriteFile(rtkPath, rtkBinData, 0755); err != nil {
		// handle error
			log.Fatalf("failed to initialize embedded optimizer: %v", err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(rtkPath, 0755); err != nil {
				// handle error
				log.Fatalf("failed to initialize embedded optimizer: %v", err)
			}
		}
        // Run init
        cmd := exec.Command(rtkPath, "init", "--local")
        cmd.Dir = shadowDir
        cmd.Env = append(os.Environ(), "RTK_NONINTERACTIVE=1")
        cmd.Run() // ignore error
    }

	 // --- SILENT GRAPH BUILD ---
	neuragraphPath := filepath.Join(assetDir, "neuragraph.exe")
	if err := os.WriteFile(neuragraphPath, assets.Neuragraph, 0755); err != nil{
		log.Fatalf("failed to initialize embedded optimizer: %v", err)
	}

	
	// BUG FIX 6: project-specific cache so multiple projects don't share a cache.
	projHash := fmt.Sprintf("%x", md5Hash(projectDir))[:8]
	cacheDir := filepath.Join(os.Getenv("APPDATA"), "neurabox", "neuragraph-cache", projHash)
	_ = os.MkdirAll(cacheDir, 0755)

	// BUG FIX 4: use bufio.Scanner so multi-word input like
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

	runtime := sandbox.NewPrimitiveEngine()
	ctx := context.Background()


	fmt.Println("Launching isolated shadow workspace view")
	openCmd := exec.Command("cmd.exe", "/c", "code", shadowDir)
	_ = openCmd.Start()

	// Single grouped defer — all cleanup in explicit order.
	// gainCmd must run BEFORE assetDir is deleted (rtkPath lives inside it).
	defer func() {
		_ = runtime.Destroy(ctx)
		os.RemoveAll(shadowDir)
		os.RemoveAll(assetDir) // last — after gainCmd finished using rtkPath
	}()

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
	for _, check := range cfg.Checks {
		fmt.Printf("Running guardrail: %s... ", check.CName)
		output, checkErr := runtime.RunSilentCheck(ctx, shadowDir, []string{"sh", "-c", check.Command})

		// On Windows, POSIX-style guardrail commands (e.g. "touch /usr/bin/virus ||
		// echo 'Safe: no files created'") get translated to cmd.exe which exits
		// non-zero on POSIX syntax it doesn't understand — every check would falsely
		// FAIL. Detect the "Safe:" sentinel that the || echo fallback emits, and treat
		// its presence as a pass regardless of exit code.
		if checkErr != nil && !strings.Contains(output, "Safe:") {
			fmt.Printf("FAILED\nReason: Policy violation detected. Outbound block applied.\n")
			fmt.Printf("Output: %s\n", strings.TrimSpace(output))
			allPassed = false
			break
		}
		fmt.Println("✅")
	}


	audit := Types.AuditEntry{
		ID:       "proxy-" + filepath.Base(shadowDir), // Unique session trace string
		TestPass: allPassed,
		Approved: false,
		Purpose:   purpose,            // Tracks exactly what command the user typed for the agent
		Agent:    agentLabel,
		Files:    changedFiles,               // Tracks string slice of every single added/modified file
		BlockList: cfg.Blocks,
	}

	if allPassed {
		fmt.Println("\n--- NEURABOX AUDIT GATE ---")
		fmt.Println("Guardrails: ALL PASSED")
		if promptYN("do you want to see the git Diff?") {
			cmd := exec.Command("git", "--no-pager", "diff", "--no-index", shadowDir, projectDir)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		}
		if promptYN("Confirm export to your real project?") {
			exported,_ := sandbox.ExportChangesTracked(shadowDir, projectDir, nil)
			fmt.Println("Project updated")
			audit.Approved = true	
			printInstallReminder(exported)
		} else {
			fmt.Println("Export canceled, shadow workspace discarded")
		}
	}
	sandbox.AuditLog(&audit)
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