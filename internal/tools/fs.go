package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// getStringArg extracts a string argument from the tool argument map.
func getStringArg(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid '%s' argument", key)
	}
	return val, nil
}

// ReadFileTool constructs the read_file tool scoped to workspaceDir.
func ReadFileTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "read_file",
		Category:    CategorySensory,
		Description: "Read the contents of a file from the local filesystem with optional line slicing and byte windowing. Supports pagination for large files.",
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
					"description": "Optional maximum byte budget for output (default: 8192)",
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

			offset := 1
			if o, ok := args["offset"]; ok {
				switch v := o.(type) {
				case float64:
					if int(v) > 0 {
						offset = int(v)
					}
				case int:
					if v > 0 {
						offset = v
					}
				case string:
					if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
						offset = parsed
					}
				}
			}

			limit := 150
			if l, ok := args["limit"]; ok {
				switch v := l.(type) {
				case float64:
					if int(v) > 0 {
						limit = int(v)
					}
				case int:
					if v > 0 {
						limit = v
					}
				case string:
					if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
						limit = parsed
					}
				}
			}

			maxBytes := 8192
			if mb, ok := args["max_bytes"]; ok {
				switch v := mb.(type) {
				case float64:
					if int(v) > 0 {
						maxBytes = int(v)
					}
				case int:
					if v > 0 {
						maxBytes = v
					}
				case string:
					if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
						maxBytes = parsed
					}
				}
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
				if hitByteLimit {
					paginationHint = fmt.Sprintf(" (Byte budget reached. To continue, call read_file with offset=%d)", endLine+1)
				} else {
					paginationHint = fmt.Sprintf(" (To continue, call read_file with offset=%d)", endLine+1)
				}
			}

			fmt.Fprintf(&sb, "[Lines %d-%d of %d (Showing %.1f KB)%s]\n\n", offset, endLine, totalLines, float64(totalBytes)/1024.0, paginationHint)
			sb.WriteString(strings.Join(lines, "\n"))

			return sb.String(), nil
		},
	}
}

// WriteFileTool constructs the write_file tool scoped to workspaceDir.
func WriteFileTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "write_file",
		Category:    CategoryMutate,
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
			safePath, err := ValidateSafePath(ws, path)
			if err != nil {
				return "", err
			}

			overwrite := false
			if ov, ok := args["overwrite"]; ok {
				switch v := ov.(type) {
				case bool:
					overwrite = v
				case string:
					overwrite = strings.ToLower(v) == "true"
				}
			}

			if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
				return "", fmt.Errorf("failed to create directories: %w", err)
			}

			action := "created"
			if _, err := os.Stat(safePath); err == nil {
				if !overwrite {
					return "", fmt.Errorf("file already exists: %s (to overwrite completely, set overwrite=true, or use edit_file / patch_file to modify)", path)
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
func AppendFileTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "append_file",
		Category:    CategoryMutate,
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
			safePath, err := ValidateSafePath(ws, path)
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

// EditFileTool constructs the edit_file tool scoped to workspaceDir.
func EditFileTool(workspaceDir string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	return Tool{
		Name:        "edit_file",
		Category:    CategoryMutate,
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

			safePath, err := ValidateSafePath(ws, path)
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
					search, err = getStringArg(args, "search_block")
				}
				if err != nil {
					return "", fmt.Errorf("missing 'search' parameter: %w", err)
				}
				replace, err := getStringArg(args, "replace")
				if err != nil {
					replace, err = getStringArg(args, "replace_block")
				}
				if err != nil {
					return "", fmt.Errorf("missing 'replace' parameter: %w", err)
				}
				if !strings.Contains(content, search) {
					return fmt.Sprintf("search string not found in file '%s'. Ensure exact match including whitespace/indentation.", path), nil
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

			return fmt.Sprintf("file '%s' edited successfully (mode: %s)", path, mode), nil
		},
	}
}
