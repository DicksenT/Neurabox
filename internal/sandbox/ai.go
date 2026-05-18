package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai" // Use the OpenAI compatible client
)

func (m *Manager) getWorkspaceFiles(shadowDir string) []string{
    var fileList []string
    filepath.WalkDir(shadowDir, func(path string, d os.DirEntry, err error) error{
        if !d.IsDir(){
            rel, _ := filepath.Rel(shadowDir, path)
            fileList = append(fileList, rel)
        }
        return nil
    })
    return fileList
}

func (m *Manager) AskAI(ctx context.Context, prompt string, shadowDir string) (string, string, error) {
    err := godotenv.Load()
    if(err != nil){
        log.Fatal("error loading .env files")
    }
    workspaceFiles := m.getWorkspaceFiles(shadowDir)
    key := os.Getenv("API_KEY")
    baseUrl := os.Getenv("AI_BASE_URL")
    model := os.Getenv("AI_MODEL")
    config := openai.DefaultConfig(key) // Replace with your real key
    config.BaseURL = baseUrl

    re := regexp.MustCompile(`\b[\w\-\./]+\.[a-zA-Z0-9]{2,4}\b`)
    matches := re.FindAllString(prompt, -1)

    var contextFiles strings.Builder
    for _, path := range workspaceFiles{
        if(slices.Contains(matches, filepath.Base(path))){
            content, _ := os.ReadFile(filepath.Join(shadowDir, path))
            contextFiles.WriteString(fmt.Sprintf("### FILE: %s\n", path))
            contextFiles.WriteString("```\n")
            contextFiles.WriteString(string(content))
            contextFiles.WriteString("\n```\n\n")
        }
    }

    client := openai.NewClientWithConfig(config)

    // 2. Prepare the System Prompt (Your "Airlock" rules)
    systemPrompt := fmt.Sprintf(`You are a SECURE coding assistant. 
    Your root workspace is: %s. 
    Content of related files: %s
    RULE: Always provide code in markdown blocks. 
    RULE: The first line of the block MUST be the relative file path.
    RULE: Be straightforward, no chitchat required
    Example: 
    `+"```"+`go src/main.go
    package main...
    `+"```", shadowDir, contextFiles.String())

    // 3. Create the Request
    resp, err := client.CreateChatCompletion(
        ctx,
        openai.ChatCompletionRequest{
            Model: model, 
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
            Temperature: 0.2,
        },
    )

    if err != nil {
        return "", "",fmt.Errorf("API error: %v",err)
    }

    return resp.Choices[0].Message.Content, model,nil

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