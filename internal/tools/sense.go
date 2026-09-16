package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SensoryTools returns the standard suite of read-only sensory tools.
func SensoryTools(workspaceDir, primaryWorkspaceDir string) []Tool {
	return []Tool{
		ReadFileTool(workspaceDir, primaryWorkspaceDir),
		ListDirectoryTool(workspaceDir, primaryWorkspaceDir),
		ListFilesRecursiveTool(workspaceDir, primaryWorkspaceDir),
		GrepSearchTool(workspaceDir, primaryWorkspaceDir),
	}
}

// walkSensoryTree safely traverses a directory tree up to maxEntries, automatically skipping
// .git, node_modules, vendor, and any directory matching SensitivePathPatterns.
func walkSensoryTree(root string, maxEntries int, onFile func(relPath, absPath string) error) error {
	count := 0
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip unreadable paths gracefully
		}

		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			if _, quarantined := isQuarantinedPath(name); quarantined {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip quarantined files
		if _, quarantined := isQuarantinedPath(name); quarantined {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		count++
		if maxEntries > 0 && count > maxEntries {
			return fmt.Errorf("too many entries found (limit %d)", maxEntries)
		}

		return onFile(rel, path)
	})
}

// ReadFileTool constructs the read_file tool scoped to workspaceDir.
func ReadFileTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	return Tool{
		Name:        "read_file",
		Category:    CategorySensory,
		Description: "Read the contents of a file from the local filesystem with optional line slicing and byte windowing. Supports pagination for large files. If a file is truncated, inspect the pagination header and call read_file again with offset set to the next offset indicated.",
		Interactive: false,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to read",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Optional line number to start reading from (1-indexed, default: 1)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Optional maximum number of lines to read (default: 150)",
				},
				"max_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Optional maximum byte budget for output (default: 65536)",
				},
			},
			"required": []string{"path"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, safePath, err := resolveToolPath(args, ws, prim)
			if err != nil {
				return "", err
			}

			offset := getIntArg(args, "offset", 1)
			limit := getIntArg(args, "limit", 150)
			maxBytes := getIntArg(args, "max_bytes", 65536)

			info, err := os.Stat(safePath)
			if err != nil {
				return "", fmt.Errorf("failed to access file: %w", err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("'%s' is a directory, not a file (use list_directory instead)", path)
			}

			file, err := os.Open(safePath)
			if err != nil {
				return "", fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			var lines []string
			scanner := bufio.NewScanner(file)
			buf := make([]byte, 64*1024)
			scanner.Buffer(buf, 1024*1024)

			lineNum := 0
			totalBytes := 0
			hitByteLimit := false
			endLine := 0

			for scanner.Scan() {
				lineNum++
				if lineNum < offset {
					continue
				}
				if len(lines) >= limit {
					break
				}

				line := scanner.Text()
				const maxSingleLineChars = 2000
				if len(line) > maxSingleLineChars {
					line = line[:maxSingleLineChars] + fmt.Sprintf(" ... [line truncated, %d chars remaining]", len(line)-maxSingleLineChars)
				}

				lineBytes := len(line) + 1
				if totalBytes+lineBytes > maxBytes && len(lines) > 0 {
					hitByteLimit = true
					break
				}

				lines = append(lines, line)
				totalBytes += lineBytes
				endLine = lineNum
			}

			totalLines := lineNum
			for scanner.Scan() {
				totalLines++
			}
			if err := scanner.Err(); err != nil {
				return "", fmt.Errorf("error reading file: %w", err)
			}

			if endLine == 0 && totalLines > 0 && offset > totalLines {
				return fmt.Sprintf("[Offset %d exceeds total file lines (%d)]", offset, totalLines), nil
			}

			var sb strings.Builder
			var paginationHint string
			if endLine < totalLines {
				remainingLines := totalLines - endLine
				if hitByteLimit {
					paginationHint = fmt.Sprintf(" (Byte budget reached; %d lines remaining. To read further, call read_file with path: %q, offset: %d)", remainingLines, path, endLine+1)
				} else {
					paginationHint = fmt.Sprintf(" (Limit reached; %d lines remaining. To read further, call read_file with path: %q, offset: %d)", remainingLines, path, endLine+1)
				}
			}

			fmt.Fprintf(&sb, "[Lines %d-%d of %d (Showing %.1f KB)%s]\n\n", offset, endLine, totalLines, float64(totalBytes)/1024.0, paginationHint)
			sb.WriteString(strings.Join(lines, "\n"))

			return sb.String(), nil
		},
	}
}

// ListDirectoryTool constructs the list_directory tool scoped to workspaceDir.
func ListDirectoryTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

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
			searchPath := "."
			if p, ok := args["path"].(string); ok && p != "" {
				searchPath = p
			}
			safePath, err := ValidateSafePath(ws, searchPath, prim)
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
func GrepSearchTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

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

			safePath, err := ValidateSafePath(ws, searchPath, prim)
			if err != nil {
				return "", err
			}

			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex pattern: %w", err)
			}

			var results []string
			err = walkSensoryTree(safePath, 100, func(relPath, absPath string) error {
				if includePattern != "" {
					matched, err := filepath.Match(includePattern, filepath.Base(absPath))
					if err != nil || !matched {
						return nil
					}
				}

				content, err := os.ReadFile(absPath)
				if err != nil {
					return nil
				}

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
func ListFilesRecursiveTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

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
			safePath, err := ValidateSafePath(ws, searchPath, prim)
			if err != nil {
				return "", err
			}

			var results []string
			err = walkSensoryTree(safePath, 500, func(relPath, absPath string) error {
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
