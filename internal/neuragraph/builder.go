// internal/neuragraph/builder.go
package neuragraph

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildASTGraph runs the vendored Python script and returns path to graph.json.
func BuildASTGraph(projectDir string) (string, error) {
    // Locate the vendored script
    exe, _ := os.Executable()
    vendorScript := filepath.Join(filepath.Dir(exe), "internal", "neuragraph", "neuragraph_ast.py")
    // Fallback to current directory if not in production
    if _, err := os.Stat(vendorScript); err != nil {
        vendorScript = filepath.Join(".", "internal", "neuragraph", "neuragraph_ast.py")
    }

    outDir := filepath.Join(projectDir, "neuragraph-out")
    outFile := filepath.Join(outDir, "graph.json")
    if err := os.MkdirAll(outDir, 0755); err != nil {
        return "", err
    }

    // Run Python script
    cmd := exec.Command("python", vendorScript, projectDir, outFile)
    cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Dir(vendorScript))
    if out, err := cmd.CombinedOutput(); err != nil {
        return "", fmt.Errorf("neuragraph failed: %s\n%s", err, out)
    }
    return outFile, nil
}