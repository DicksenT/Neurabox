package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/DicksenT/neurabox/internal/session"
	// ... your other imports
)
const defaultPolicy = `version: "1.0"
name: "neurabox-default"
image: "node:20"

# Only these paths exist inside the AI's world
mounts:
  - source: "."
    target: "/workspace"
    mode: "ro" 
    #modify source below to your working directory/folder like component ,etc...
  - source: "./src"
    target: "/workspace/src"
    mode: "rw"
  - source: "./tests"
    target: "/workspace/tests"
    mode: "rw"
# Hard gates the AI must pass before the code is "trusted"
checks:
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