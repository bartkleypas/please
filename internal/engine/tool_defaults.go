package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var allowedCommands = []string{
	"git", "go", "ls", "grep", "cat", "echo", "find", "pwd", "date", "rm",
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

// getStringArg is a helper to extract string arguments from tool arguments
func getStringArg(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid '%s' argument", key)
	}
	return val, nil
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
				path, err := getStringArg(args, "path")
				if err != nil {
					return "", err
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
				path, err := getStringArg(args, "path")
				if err != nil {
					return "", err
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
				command, err := getStringArg(args, "command")
				if err != nil {
					return "", err
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
		{
			Name:        "patch_file",
			Description: "Search for a string in a file and replace it with another string",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to patch",
					},
					"search": map[string]interface{}{
						"type":        "string",
						"description": "The string to search for",
					},
					"replace": map[string]interface{}{
						"type":        "string",
						"description": "The string to replace it with",
					},
				},
				"required": []string{"path", "search", "replace"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				path, err := getStringArg(args, "path")
				if err != nil {
					return "", err
				}
				search, err := getStringArg(args, "search")
				if err != nil {
					return "", err
				}
				replace, err := getStringArg(args, "replace")
				if err != nil {
					return "", err
				}

				safePath, err := validatePath(path)
				if err != nil {
					return "", err
				}

				content, err := os.ReadFile(safePath)
				if err != nil {
					return "", fmt.Errorf("failed to read file: %w", err)
				}

				newContent := strings.Replace(string(content), search, replace, 1)
				if newContent == string(content) {
					return "search string not found in file", nil
				}

				err = os.WriteFile(safePath, []byte(newContent), 0644)
				if err != nil {
					return "", fmt.Errorf("failed to write file: %w", err)
				}

				return "file patched successfully", nil
			},
		},
		{
			Name:        "edit_file",
			Description: "Advanced text editing tool supporting regex, line replacement, and insertions",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to edit",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"replace_string", "replace_regex", "replace_line", "insert_after"},
						"description": "The mode of operation",
					},
					"search": map[string]interface{}{
						"type":        "string",
						"description": "The string or regex pattern to search for",
					},
					"replace": map[string]interface{}{
						"type":        "string",
						"description": "The replacement string",
					},
					"line_number": map[string]interface{}{
						"type":        "integer",
						"description": "Required if mode is 'replace_line'",
					},
				},
				"required": []string{"path", "mode"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				path, err := getStringArg(args, "path")
				if err != nil {
					return "", err
				}
				mode, err := getStringArg(args, "mode")
				if err != nil {
					return "", err
				}

				safePath, err := validatePath(path)
				if err != nil {
					return "", err
				}

				contentBytes, err := os.ReadFile(safePath)
				if err != nil {
					return "", fmt.Errorf("failed to read file: %w", err)
				}
				content := string(contentBytes)
				lines := strings.Split(content, "\n")

				var newContent string

				switch mode {
				case "replace_string":
					search, err := getStringArg(args, "search")
					if err != nil {
						return "", err
					}
					replace, err := getStringArg(args, "replace")
					if err != nil {
						return "", err
					}
					newContent = strings.Replace(content, search, replace, 1)

				case "replace_regex":
					search, err := getStringArg(args, "search")
					if err != nil {
						return "", err
					}
					replace, err := getStringArg(args, "replace")
					if err != nil {
						return "", err
					}
					re, err := regexp.Compile(search)
					if err != nil {
						return "", fmt.Errorf("invalid regex: %w", err)
					}
					newContent = re.ReplaceAllString(content, replace)

				case "replace_line":
					lineNumVal, ok := args["line_number"]
					if !ok {
						return "", fmt.Errorf("missing line_number for replace_line mode")
					}
					lineNum, err := strconv.Atoi(fmt.Sprintf("%v", lineNumVal))
					if err != nil {
						return "", fmt.Errorf("invalid line_number: %w", err)
					}
					if lineNum < 1 || lineNum > len(lines) {
						return "", fmt.Errorf("line number %d out of range (1-%d)", lineNum, len(lines))
					}
					replace, err := getStringArg(args, "replace")
					if err != nil {
						return "", err
					}
					lines[lineNum-1] = replace
					newContent = strings.Join(lines, "\n")

				case "insert_after":
					search, err := getStringArg(args, "search")
					if err != nil {
						return "", err
					}
					replace, err := getStringArg(args, "replace")
					if err != nil {
						return "", err
					}
					found := false
					for i, line := range lines {
						if strings.Contains(line, search) {
							newLines := append(lines[:i+1], append([]string{replace}, lines[i+1:]...)...)
							lines = newLines
							found = true
							break
						}
					}
					if !found {
						return "search pattern not found", nil
					}
					newContent = strings.Join(lines, "\n")

				default:
					return "", fmt.Errorf("unknown mode: %s", mode)
				}

				err = os.WriteFile(safePath, []byte(newContent), 0644)
				if err != nil {
					return "", fmt.Errorf("failed to write file: %w", err)
				}

				return "file edited successfully", nil
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
