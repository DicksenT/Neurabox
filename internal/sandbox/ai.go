package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
    "github.com/sashabaranov/go-openai" // Use the OpenAI compatible client
)

func (m *Manager) AskAI(ctx context.Context, prompt string, shadowDir string) (string, error) {
    // 1. Configure for DeepSeek
    config := openai.DefaultConfig("sk-7663d06d3ecf482582b501f9e3ddc1b0") // Replace with your real key
    config.BaseURL = "https://api.deepseek.com"

    client := openai.NewClientWithConfig(config)

    // 2. Prepare the System Prompt (Your "Airlock" rules)
    systemPrompt := fmt.Sprintf(`You are a SECURE coding assistant. 
    Your root workspace is: %s. 
    RULE: Always provide code in markdown blocks. 
    RULE: The first line of the block MUST be the relative file path.
    RULE: Be straightforward, no chitchat required
    Example: 
    `+"```"+`go src/main.go
    package main...
    `+"```", shadowDir)

    // 3. Create the Request
    resp, err := client.CreateChatCompletion(
        ctx,
        openai.ChatCompletionRequest{
            Model: "deepseek-chat", // Or "deepseek-reasoner" for deep thinking
            Messages: []openai.ChatCompletionMessage{
                {
                    Role:    openai.ChatMessageRoleSystem,
                    Content: systemPrompt,
                },
                {
                    Role:    openai.ChatMessageRoleUser,
                    Content: prompt,
                },
            },
            Temperature: 0.2, // Low temperature for consistent code structure
        },
    )

    if err != nil {
        return "", fmt.Errorf("DeepSeek API error: %v", err)
    }

    return resp.Choices[0].Message.Content, nil

}

func (m *Manager) ApplyAI(shadowDir string, aiResponse string) ( []string, error) {
    // Regex optimized to find the path on the first line of the block
    re := regexp.MustCompile("(?s)```[a-zA-Z]*\\s+(.+?)\\n(.*?)\\n```")
    matches := re.FindAllStringSubmatch(aiResponse, -1)
    var files []string
    for _, match := range matches {
        relPath := filepath.Clean(strings.TrimSpace(match[1]))
        content := match[2]

        // --- SECURITY CHECK: Prevent Path Traversal ---
        // Ensure the path doesn't try to go "up" out of the shadowDir
        if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
            return nil,fmt.Errorf("SECURITY ALERT: AI tried to write outside workspace: %s", relPath)
        }

        fullPath := filepath.Join(shadowDir, relPath)

        // Double check: Does the final path still start with the shadowDir?
        if !strings.HasPrefix(fullPath, filepath.Clean(shadowDir)) {
            return nil,fmt.Errorf("SECURITY ALERT: Path resolution escape: %s", relPath)
        }
        // ----------------------------------------------

        if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
            return nil, err
        }

        if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
            return nil, err
        }
        files = append(files, relPath)
        fmt.Printf("[PARSER] Verified & Wrote: %s\n", relPath)
    }
    return files, nil
}