package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
)

// MutateTools returns the standard suite of workspace state-modifying tools.
func MutateTools(workspaceDir, primaryWorkspaceDir string) []Tool {
	return []Tool{
		WriteFileTool(workspaceDir, primaryWorkspaceDir),
		AppendFileTool(workspaceDir, primaryWorkspaceDir),
		EditFileTool(workspaceDir, primaryWorkspaceDir),
		DeleteFileTool(workspaceDir, primaryWorkspaceDir),
	}
}

// WriteFileTool constructs the write_file tool scoped to workspaceDir.
func WriteFileTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	return Tool{
		Name:        "write_file",
		Category:    domain.CategoryMutate,
		Description: "Create a new file with content, or optionally overwrite an existing file when overwrite=true.",
		Interactive: true,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to write to",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to write to the file",
				},
				"overwrite": map[string]interface{}{
					"type":        "boolean",
					"description": "Optional: if true, overwrites the file if it already exists. Defaults to false.",
				},
			},
			"required": []string{"path", "content"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, err := getStringArg(args, "path")
			if err != nil {
				return "", err
			}
			content, err := getStringArg(args, "content")
			if err != nil {
				return "", err
			}
			safePath, err := ValidateSafePath(ws, path, prim)
			if err != nil {
				return "", err
			}

			overwrite := getBoolArg(args, "overwrite", false)

			if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
				return "", fmt.Errorf("failed to create directories: %w", err)
			}

			action := "created"
			if _, err := os.Stat(safePath); err == nil {
				if !overwrite {
					return "", fmt.Errorf("file already exists: %s (to overwrite completely, set overwrite=true, or use edit_file to modify)", path)
				}
				action = "overwritten"
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("error checking file: %w", err)
			}

			byteContent := []byte(content)
			if err := os.WriteFile(safePath, byteContent, 0644); err != nil {
				return "", fmt.Errorf("failed to write file: %w", err)
			}
			lineCount := len(strings.Split(content, "\n"))
			return fmt.Sprintf("file '%s' %s successfully (%d bytes, %d lines)", path, action, len(byteContent), lineCount), nil
		},
	}
}

// AppendFileTool constructs the append_file tool scoped to workspaceDir.
func AppendFileTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	return Tool{
		Name:        "append_file",
		Category:    domain.CategoryMutate,
		Description: "Append content to the end of a file on the local filesystem. Creates the file and parent directories if they do not exist. Automatically handles line boundary separation without creating redundant blank lines.",
		Interactive: true,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to append to",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to append to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, err := getStringArg(args, "path")
			if err != nil {
				return "", err
			}
			content, err := getStringArg(args, "content")
			if err != nil {
				return "", err
			}
			safePath, err := ValidateSafePath(ws, path, prim)
			if err != nil {
				return "", err
			}

			if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
				return "", fmt.Errorf("failed to create directories: %w", err)
			}

			stat, err := os.Stat(safePath)
			if err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("error checking file: %w", err)
			}

			if os.IsNotExist(err) {
				byteContent := []byte(content)
				if err := os.WriteFile(safePath, byteContent, 0644); err != nil {
					return "", fmt.Errorf("failed to create file via append: %w", err)
				}
				lineCount := len(strings.Split(content, "\n"))
				return fmt.Sprintf("file '%s' created successfully via append (%d bytes, %d lines)", path, len(byteContent), lineCount), nil
			}

			prefix := ""
			if stat.Size() > 0 {
				f, err := os.Open(safePath)
				if err == nil {
					lastByte := make([]byte, 1)
					if _, err := f.ReadAt(lastByte, stat.Size()-1); err == nil {
						if lastByte[0] != '\n' {
							prefix = "\n"
						}
					}
					f.Close()
				}
			}

			toAppend := content
			if prefix == "" && strings.HasPrefix(toAppend, "\n") {
				toAppend = strings.TrimPrefix(toAppend, "\n")
			}

			data := []byte(prefix + toAppend)
			f, err := os.OpenFile(safePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
			if err != nil {
				return "", fmt.Errorf("failed to open file for append: %w", err)
			}
			defer f.Close()

			if _, err := f.Write(data); err != nil {
				return "", fmt.Errorf("failed to append to file: %w", err)
			}

			newSize := stat.Size() + int64(len(data))
			addedLines := len(strings.Split(toAppend, "\n"))
			return fmt.Sprintf("file '%s' appended successfully (added %d bytes, %d lines; total size: %d bytes)", path, len(data), addedLines, newSize), nil
		},
	}
}

// applyReplaceString replaces a single exact occurrence of search in content.
func applyReplaceString(content, search, replace, path string) (string, error) {
	if search == "" {
		return "", fmt.Errorf("search parameter cannot be empty")
	}
	matchCount := strings.Count(content, search)
	if matchCount == 0 {
		return "", fmt.Errorf("search string not found in file '%s'. Ensure exact match including whitespace/indentation", path)
	}
	if matchCount > 1 {
		return "", fmt.Errorf("search string matched %d occurrences in '%s'. Provide more surrounding context lines to uniquely identify the target", matchCount, path)
	}
	return strings.Replace(content, search, replace, 1), nil
}

// applyReplaceRegex replaces matches of pattern in content.
func applyReplaceRegex(content, pattern, replace, path string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("search parameter cannot be empty")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	if !re.MatchString(content) {
		return "", fmt.Errorf("regex pattern '%s' matched no content in file '%s'", pattern, path)
	}
	return re.ReplaceAllString(content, replace), nil
}

// applyReplaceLine replaces a specific 1-indexed line in lines.
func applyReplaceLine(lines []string, lineNumVal interface{}, replace, path string) (string, error) {
	if lineNumVal == nil {
		return "", fmt.Errorf("missing line_number for replace_line mode")
	}
	lineNum, err := strconv.Atoi(fmt.Sprintf("%v", lineNumVal))
	if err != nil {
		return "", fmt.Errorf("invalid line_number: %w", err)
	}
	if lineNum < 1 || lineNum > len(lines) {
		return "", fmt.Errorf("line number %d out of range (1-%d) in file '%s'", lineNum, len(lines), path)
	}
	lines[lineNum-1] = replace
	return strings.Join(lines, "\n"), nil
}

// applyInsertAfter inserts replace immediately after the first line containing search.
func applyInsertAfter(lines []string, search, replace, path string) (string, error) {
	if search == "" {
		return "", fmt.Errorf("search parameter cannot be empty")
	}
	for i, line := range lines {
		if strings.Contains(line, search) {
			newLines := append(lines[:i+1], append([]string{replace}, lines[i+1:]...)...)
			return strings.Join(newLines, "\n"), nil
		}
	}
	return "", fmt.Errorf("search pattern '%s' not found in file '%s'", search, path)
}

// EditFileTool constructs the edit_file tool scoped to workspaceDir.
func EditFileTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	return Tool{
		Name:        "edit_file",
		Category:    domain.CategoryMutate,
		Description: "Surgical in-place text editing tool supporting search & replace (default), regex, line replacement, and insertions",
		Interactive: true,
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
					"description": "Optional: the mode of operation (defaults to 'replace_string')",
				},
				"search": map[string]interface{}{
					"type":        "string",
					"description": "The string or regex pattern to search for (also accepts search_block)",
				},
				"replace": map[string]interface{}{
					"type":        "string",
					"description": "The replacement string or block (also accepts replace_block)",
				},
				"line_number": map[string]interface{}{
					"type":        "integer",
					"description": "Required if mode is 'replace_line'",
				},
			},
			"required": []string{"path"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, err := getStringArg(args, "path")
			if err != nil {
				return "", err
			}
			mode := "replace_string"
			if m, ok := args["mode"].(string); ok && m != "" {
				mode = m
			}

			safePath, err := ValidateSafePath(ws, path, prim)
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
			var editErr error

			switch mode {
			case "replace_string":
				search, sErr := getStringArg(args, "search")
				if sErr != nil {
					search, sErr = getStringArg(args, "search_block")
				}
				if sErr != nil {
					return "", fmt.Errorf("missing 'search' parameter: %w", sErr)
				}
				replace, rErr := getStringArg(args, "replace")
				if rErr != nil {
					replace, rErr = getStringArg(args, "replace_block")
				}
				if rErr != nil {
					return "", fmt.Errorf("missing 'replace' parameter: %w", rErr)
				}
				newContent, editErr = applyReplaceString(content, search, replace, path)

			case "replace_regex":
				search, sErr := getStringArg(args, "search")
				if sErr != nil {
					return "", sErr
				}
				replace, rErr := getStringArg(args, "replace")
				if rErr != nil {
					return "", rErr
				}
				newContent, editErr = applyReplaceRegex(content, search, replace, path)

			case "replace_line":
				replace, rErr := getStringArg(args, "replace")
				if rErr != nil {
					return "", rErr
				}
				newContent, editErr = applyReplaceLine(lines, args["line_number"], replace, path)

			case "insert_after":
				search, sErr := getStringArg(args, "search")
				if sErr != nil {
					return "", sErr
				}
				replace, rErr := getStringArg(args, "replace")
				if rErr != nil {
					return "", rErr
				}
				newContent, editErr = applyInsertAfter(lines, search, replace, path)

			default:
				return "", fmt.Errorf("unknown mode: %s", mode)
			}

			if editErr != nil {
				return "", editErr
			}

			if err := os.WriteFile(safePath, []byte(newContent), 0644); err != nil {
				return "", fmt.Errorf("failed to write file: %w", err)
			}

			return fmt.Sprintf("file '%s' edited successfully (mode: %s)", path, mode), nil
		},
	}
}

// DeleteFileTool constructs the delete_file tool scoped to workspaceDir.
func DeleteFileTool(workspaceDir ...string) Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	return Tool{
		Name:        "delete_file",
		Category:    domain.CategoryMutate,
		Description: "Delete a file from the workspace. Path must be inside workspace and not match quarantined patterns.",
		Interactive: true,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to delete",
				},
			},
			"required": []string{"path"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, safePath, err := resolveToolPath(args, ws, prim)
			if err != nil {
				return "", err
			}

			info, err := os.Stat(safePath)
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("file does not exist: %s", path)
				}
				return "", fmt.Errorf("failed to access file: %w", err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("'%s' is a directory, not a file", path)
			}

			if err := os.Remove(safePath); err != nil {
				return "", fmt.Errorf("failed to delete file: %w", err)
			}

			return fmt.Sprintf("file '%s' deleted successfully", path), nil
		},
	}
}
