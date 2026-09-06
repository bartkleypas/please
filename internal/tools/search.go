package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ListDirectoryTool constructs the list_directory tool scoped to workspaceDir.
func ListDirectoryTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "list_directory",
		Category:    CategorySensory,
		Description: "List the contents of a directory on the local filesystem",
		Interactive: false,
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
			safePath, err := ValidateSafePath(ws, path)
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
	}
}

// GrepSearchTool constructs the grep_search tool scoped to workspaceDir.
func GrepSearchTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "grep_search",
		Category:    CategorySensory,
		Description: "Search for a pattern in files within a directory (recursive)",
		Interactive: false,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "The regex pattern to search for",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The directory to search in (defaults to '.')",
				},
				"include": map[string]interface{}{
					"type":        "string",
					"description": "Optional glob pattern for files to include (e.g., '*.go')",
				},
			},
			"required": []string{"pattern"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			pattern, err := getStringArg(args, "pattern")
			if err != nil {
				return "", err
			}
			searchPath := "."
			if p, ok := args["path"].(string); ok && p != "" {
				searchPath = p
			}
			includePattern := ""
			if inc, ok := args["include"].(string); ok {
				includePattern = inc
			}

			safePath, err := ValidateSafePath(ws, searchPath)
			if err != nil {
				return "", err
			}

			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex pattern: %w", err)
			}

			var results []string
			err = filepath.Walk(safePath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					if info.Name() == ".git" {
						return filepath.SkipDir
					}
					return nil
				}

				if includePattern != "" {
					matched, err := filepath.Match(includePattern, info.Name())
					if err != nil || !matched {
						return nil
					}
				}

				content, err := os.ReadFile(path)
				if err != nil {
					return nil // Skip files we can't read
				}

				relPath, _ := filepath.Rel(safePath, path)
				lines := strings.Split(string(content), "\n")
				for i, line := range lines {
					if re.MatchString(line) {
						results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
						if len(results) > 100 {
							return fmt.Errorf("too many matches found (limit 100)")
						}
					}
				}
				return nil
			})

			if err != nil && !strings.Contains(err.Error(), "too many matches") {
				return "", fmt.Errorf("grep failed: %w", err)
			}

			if len(results) == 0 {
				return "no matches found", nil
			}

			return strings.Join(results, "\n"), nil
		},
	}
}

// ListFilesRecursiveTool constructs the list_files_recursive tool scoped to workspaceDir.
func ListFilesRecursiveTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "list_files_recursive",
		Category:    CategorySensory,
		Description: "Recursively list all files in a directory",
		Interactive: false,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The directory to list files from (defaults to '.')",
				},
			},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			searchPath := "."
			if p, ok := args["path"].(string); ok && p != "" {
				searchPath = p
			}
			safePath, err := ValidateSafePath(ws, searchPath)
			if err != nil {
				return "", err
			}

			var results []string
			err = filepath.Walk(safePath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
						return filepath.SkipDir
					}
					return nil
				}
				relPath, _ := filepath.Rel(safePath, path)
				results = append(results, relPath)
				if len(results) > 500 {
					return fmt.Errorf("too many files found (limit 500)")
				}
				return nil
			})

			if err != nil && !strings.Contains(err.Error(), "too many files") {
				return "", fmt.Errorf("list_files failed: %w", err)
			}

			return strings.Join(results, "\n"), nil
		},
	}
}
