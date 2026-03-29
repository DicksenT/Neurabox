package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
    "google.golang.org/genai"
)

func (m *Manager) AskAI(ctx context.Context, prompt string, shadowDir string) (string, error) {
	client, err := genai.NewClient(ctx,  &genai.ClientConfig{
        APIKey: "AIzaSyC3lzF22fcemAf6rkQ8MGtZbnBldbmYjYI",
        Backend: genai.BackendGeminiAPI,
    })
	if(err!=nil){
		return "", err
	}
    fmt.Println("✅ Connection to Gemini established!")
	systemPrompt := fmt.Sprintf(`You are a SECURE coding assistant. 
    Your root workspace is: %s. 
    RULE: Always provide code in markdown blocks. 
    RULE: The first line of the block MUST be the relative file path.
    Example: 
    `+"```"+`go src/main.go
    package main...
    `+"```", shadowDir)

    systemTemp := float32(0.2)
    config := &genai.GenerateContentConfig{
        SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleModel),
        Temperature: &systemTemp,
    }

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.5-flash", 
		genai.Text(prompt),
		config,
	)
    if(err != nil){
        return "", err
    }
    return result.Text(), nil

}

func (m *Manager) ApplyAI(shadowDir string, aiResponse string) error {
    // Regex optimized to find the path on the first line of the block
    re := regexp.MustCompile("(?s)```[a-zA-Z]*\\s+(.+?)\\n(.*?)\\n```")
    matches := re.FindAllStringSubmatch(aiResponse, -1)

    for _, match := range matches {
        relPath := filepath.Clean(strings.TrimSpace(match[1]))
        content := match[2]

        // --- SECURITY CHECK: Prevent Path Traversal ---
        // Ensure the path doesn't try to go "up" out of the shadowDir
        if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
            return fmt.Errorf("SECURITY ALERT: AI tried to write outside workspace: %s", relPath)
        }

        fullPath := filepath.Join(shadowDir, relPath)

        // Double check: Does the final path still start with the shadowDir?
        if !strings.HasPrefix(fullPath, filepath.Clean(shadowDir)) {
            return fmt.Errorf("SECURITY ALERT: Path resolution escape: %s", relPath)
        }
        // ----------------------------------------------

        if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
            return err
        }

        if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
            return err
        }
        fmt.Printf("[PARSER] Verified & Wrote: %s\n", relPath)
    }
    return nil
}