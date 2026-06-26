package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/DicksenT/neurabox/internal/session"
)

const defaultPolicy = `
version: "0.1"
image: "node:20-alpine"
mounts:
  - source: "."
    target: "/workspace"
    mode: "ro"
  - source: "./src"
    target: "/workspace/src"
    mode: "rw"
# files to block from agent visibility
blocks:
  - ".env"
  - "node_modules"
  - ".git"
  - "secret.json"
# The "Audit Gate" Pipeline
checks:
  - cname: "structure"
    command: "[ -d 'src/controllers' ] && [ -d 'src/routes' ]" 
  
  - cname: "no-internet-leak"
    command: "curl -m 2 google.com || echo 'Safe: No internet'"
  
  - cname: "performance-check"
    command: "timeout 5s yes > /dev/null || echo 'Safe: CPU limit hit'"
  
  - cname: "system modification"
    command: "touch /usr/bin/virus || echo 'Safe: no files created'"
`

func main() {
	// 1. Configure modern usage menu optimized purely for Agents
	flag.Usage = func() {
		fmt.Println("  Neurabox AI Agent Sandbox Proxy")
		fmt.Println("\nUsage:")
		fmt.Println("  neurabox [flags] <agent-command> [args...]")
		fmt.Println("  neurabox [flags] -- <agent-command> [args...]")
		fmt.Println("\nExamples:")
		fmt.Println("  neurabox aider --message 'add tests'")
		fmt.Println("  neurabox --hardened claude-engineer")
		fmt.Println("\nFlags:")
		flag.PrintDefaults()
		fmt.Print("  --init                             Initialize default nb-policy.yaml configuration\n")
	}

	hardenedMode := flag.Bool("hardened", false, "Enable zero-trust execution (cuts local loopback & IDE proxy layers)")
	initCmd := flag.Bool("init", false, "Initialize a default nb-policy.yaml rulebook in the current directory")
	flag.Parse()

	// 2. Handle configuration initialization
	if *initCmd {
		err := os.WriteFile("nb-policy.yaml", []byte(strings.TrimSpace(defaultPolicy)), 0644)
		if err != nil {
			fmt.Printf("❌ Error initializing policy: %v\n", err)
			return
		}
		fmt.Println("✨ Initialized nb-policy.yaml successfully. Customize your guardrails before running your agent!")
		return
	}

	// 3. Capture remaining positional parameters after flags are parsed
	// Go's flag package automatically stops at the first non-flag argument,
	// meaning everything intended for the agent falls gracefully into flag.Args().
	args := flag.Args()

	// If the user used an explicit "--" separator, strip it out cleanly
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	// Guard clause: Ensure there is actually an agent command to isolate
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	agentCmd := args

	fmt.Println("==================================================")
	fmt.Println("🛡️  NEURABOX SECURE EXECUTION PROXY INITIATED")
	fmt.Printf("🎯 Target Agent: %s\n", strings.Join(agentCmd, " "))
	fmt.Println("==================================================")

	// 4. Hand off execution control directly to the interactive proxy runtime
	session.RunProxySession(agentCmd, *hardenedMode)
}