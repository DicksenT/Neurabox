package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/DicksenT/neurabox/internal/session"
	// ... your other imports
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
  
  - cname: "eslint"
    command: "npx eslint ." # linting, please match it with your programming language

  #malicious test
  - cname: "no-internet-leak"
    command: "curl -m 2 google.com || echo 'Safe: No internet'"
  
  - cname: "performance-check"
    command: "timeout 5s yes > /dev/null || echo 'Safe: CPU limit hit'"
  
  - cname: "system modification"
    command: "touch /usr/bin/virus || echo 'Safe: no files created'"
`

func main() {
    // 1. Define flags
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
    args := flag.Args()
    if len(args) == 0 {
        fmt.Println("❌ Usage: neurabox.exe \"your instructions here\"")
        fmt.Println("💡 Tip: Use --init to create a configuration file.")
        return
    }
    userPrompt := args[0]

    // 4. Start the session using 'userPrompt'...
    session.RunSession(userPrompt)
}