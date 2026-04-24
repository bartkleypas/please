package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var allowedCommands = []string{
	"git", "go", "ls", "grep", "cat", "echo", "find", "pwd", "date",
}

func validatePath(path string) (string, error) {
	absRoot, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("failed to get absolute root: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if !strings.HasPrefix(absPath, absRoot) {
		return "", fmt.Errorf("security error: path is outside of project root")
	}

	return absPath, nil
}

// GetDefaultTools returns a list of basic tools for the application
func GetDefaultTools() []Tool {
	return []Tool{
		{
			Name:        "read_file",
			Description: "Read the contents of a file from the local filesystem",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to read",
					},
				},
				"required": []string{"path"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				path, ok := args["path"].(string)
				if !ok {
					return "", fmt.Errorf("missing or invalid 'path' argument")
				}
				safePath, err := validatePath(path)
				if err != nil {
					return "", err
				}
				content, err := os.ReadFile(safePath)
				if err != nil {
					return "", fmt.Errorf("failed to read file: %w", err)
				}
				return string(content), nil
			},
		},
		{
			Name:        "list_directory",
			Description: "List the contents of a directory on the local filesystem",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the directory to list",
					},
				},
				"required": []string{"path"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				path, ok := args["path"].(string)
				if !ok {
					return "", fmt.Errorf("missing or invalid 'path' argument")
				}
				safePath, err := validatePath(path)
				if err != nil {
					return "", err
				}
				entries, err := os.ReadDir(safePath)
				if err != nil {
					return "", fmt.Errorf("failed to list directory: %w", err)
				}

				var result []string
				for _, entry := range entries {
					suffix := ""
					if entry.IsDir() {
						suffix = "/"
					}
					result = append(result, entry.Name()+suffix)
				}

				if len(result) == 0 {
					return "(empty directory)", nil
				}

				return strings.Join(result, "\n"), nil
			},
		},
		{
			Name:        "execute_command",
			Description: "Execute a shell command and return the combined output",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to execute",
					},
				},
				"required": []string{"command"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				command, ok := args["command"].(string)
				if !ok {
					return "", fmt.Errorf("missing or invalid 'command' argument")
				}

				parts := strings.Fields(command)
				if len(parts) == 0 {
					return "", fmt.Errorf("empty command")
				}

				binary := parts[0]
				allowed := false
				for _, a := range allowedCommands {
					if binary == a {
						allowed = true
						break
					}
				}

				if !allowed {
					return "", fmt.Errorf("security error: command '%s' is not in the allow-list", binary)
				}

				// We execute using 'bash -c' for flexibility, but we've validated the primary binary.
				cmd := exec.CommandContext(ctx, "bash", "-c", command)
				// Ensure it runs in the project root
				absRoot, _ := filepath.Abs(".")
				cmd.Dir = absRoot

				output, err := cmd.CombinedOutput()
				if err != nil {
					return string(output), fmt.Errorf("command failed: %w", err)
				}

				return string(output), nil
			},
		},
	}
}

// RegisterDefaultTools adds the default toolset to the manager's registry
func (m *Manager) RegisterDefaultTools() {
	for _, t := range GetDefaultTools() {
		m.Registry.Register(t)
	}
}
