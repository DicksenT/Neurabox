package main

import (
	"flag"
	"fmt"
	"os"

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
    #files to block
blocks:
  - ".env"
  - "node_modules"
  - ".git"
  - "secret.json"
# The "Audit Gate" Pipeline
checks:
  - cname: "structure"
    command: "[ -d 'src/controllers' ] && [ -d 'src/routes' ]" # Enforces folder structure, please match it with yours
  
  #malicious test
  - cname: "no-internet-leak"
    command: "curl -m 2 google.com || echo 'Safe: No internet'"
  
  - cname: "performance-check"
    command: "timeout 5s yes > /dev/null || echo 'Safe: CPU limit hit'"
  
  - cname: "system modification"
    command: "touch /usr/bin/virus || echo 'Safe: no files created'"
  #packages that need to be installed ( only needed to do it once for local use )
  package:
  - "apt-get update && apt-get install nano -y"
`

func main() {
    // 1. Define flags
    flag.Usage = func() {
        fmt.Println("LLM Usage: neurabox.exe \"your instructions here\"")
        fmt.Println("AGENT Usage: neurabox exec -- <agent-command> [args...]")
		    fmt.Println("Agent Example: neurabox exec -- aider --message 'add tests'")
        fmt.Println("\nFlags:")
		    fmt.Print("  --init                             Initialize default policy configuration\n")
    }

    hardenedMode := flag.Bool("hardened", false, "Enable maximum security zero-trust isolation (Blocks local network extensions)")
    initCmd := flag.Bool("init", false, "Initialize a default nb-policy.yaml")
    flag.Parse()

    // 2. Handle 'init'
    if *initCmd {
        err := os.WriteFile("nb-policy.yaml", []byte(defaultPolicy), 0644)
        if err != nil {
            fmt.Printf("Error creating policy: %v\n", err)
            return
        }
        fmt.Println("Initialized nb-policy.yaml. Please check it!")
        return
    }

    // 3. Get the Prompt from the remaining arguments

    switch os.Args[1]{
      case "exec":
          promptCommand()
      default:
          agentCommand(*hardenedMode)
    }

    // 4. Start the session using 'userPrompt'...
   
}
func agentCommand(hardened bool) {
	var agentCmd []string
	splitIdx := -1

	for i, arg := range os.Args {
		if arg == "--"{
			splitIdx = i
			break
		}
	}
	if splitIdx == -1 || splitIdx == len(os.Args)-1{
		fmt.Println("Command not found, check --help for usage")
		return
	}
		agentCmd = os.Args[splitIdx+1:]
	session.RunProxySession(agentCmd, hardened)
}

func promptCommand(){
	if(len(os.Args) != 2){
		fmt.Println("Command not found, check --help for usage")
		return
	}
	session.RunSession(os.Args[1])
}

func SayHalo(name string) string {
    return fmt.Sprintf("Halo, %s!", name)
}