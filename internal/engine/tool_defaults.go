package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

var allowedCommands = StandardAllowedCommands

func validatePath(workspaceDir, path string) (string, error) {
	return ValidateSafePath(workspaceDir, path)
}

// getStringArg is a helper to extract string arguments from tool arguments
func getStringArg(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid '%s' argument", key)
	}
	return val, nil
}

// GetDefaultTools returns a list of basic tools for the application scoped to a workspace directory
func GetDefaultTools(workspaceDir ...string) []Tool {
	ws := "."
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
	}

	return []Tool{
		{
			Name:        "inspect_image",
			Description: "Read and inspect an image file's properties. Extracts format, resolution, file size, and any embedded generation metadata (such as Stable Diffusion prompts, seeds, negative prompts, and models).",
			Interactive: false,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the image file to inspect",
					},
				},
				"required": []string{"path"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				path, err := getStringArg(args, "path")
				if err != nil {
					return "", err
				}
				safePath, err := validatePath(ws, path)
				if err != nil {
					return "", err
				}

				file, err := os.Open(safePath)
				if err != nil {
					return "", fmt.Errorf("failed to open image: %w", err)
				}
				defer file.Close()

				stat, err := file.Stat()
				if err != nil {
					return "", fmt.Errorf("failed to stat image: %w", err)
				}

				cfg, format, decodeErr := image.DecodeConfig(file)
				if _, err := file.Seek(0, 0); err != nil {
					return "", fmt.Errorf("failed to seek image: %w", err)
				}

				var s strings.Builder
				fmt.Fprintf(&s, "Image Inspection Report for %s:\n", filepath.Base(path))
				if decodeErr == nil {
					fmt.Fprintf(&s, "- Format: %s\n", format)
					fmt.Fprintf(&s, "- Dimensions: %dx%d\n", cfg.Width, cfg.Height)
				} else {
					fmt.Fprintf(&s, "- Format: (unsupported by image parser: %v)\n", decodeErr)
				}
				fmt.Fprintf(&s, "- File Size: %.2f MB\n", float64(stat.Size())/(1024*1024))

				if strings.ToLower(format) == "png" {
					meta, err := ExtractPNGMetadata(file)
					if err == nil && len(meta) > 0 {
						if params, exists := meta["parameters"]; exists {
							sdMeta := ParseSDParameters(params)
							fmt.Fprintf(&s, "\nStable Diffusion Metadata Found:\n")
							if val, ok := sdMeta["sd_prompt"]; ok && val != "" {
								fmt.Fprintf(&s, "- Prompt: %s\n", val)
							}
							if val, ok := sdMeta["sd_negative_prompt"]; ok && val != "" {
								fmt.Fprintf(&s, "- Negative Prompt: %s\n", val)
							}
							if val, ok := sdMeta["sd_seed"]; ok && val != "" {
								fmt.Fprintf(&s, "- Seed: %s\n", val)
							}
							if val, ok := sdMeta["sd_sampler"]; ok && val != "" {
								fmt.Fprintf(&s, "- Sampler: %s\n", val)
							}
							if val, ok := sdMeta["sd_cfg_scale"]; ok && val != "" {
								fmt.Fprintf(&s, "- CFG Scale: %s\n", val)
							}
							if val, ok := sdMeta["sd_steps"]; ok && val != "" {
								fmt.Fprintf(&s, "- Steps: %s\n", val)
							}
							if val, ok := sdMeta["sd_model"]; ok && val != "" {
								fmt.Fprintf(&s, "- Model: %s\n", val)
							}
						}
					}
				}
				return s.String(), nil
			},
		},
		{
			Name:        "read_file",
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
				safePath, err := validatePath(ws, path)
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
					// Check per-line length limit (2000 chars) for minified/giant lines
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

				// Count total lines in file
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
		},
		{
			Name:        "write_file",
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
				safePath, err := validatePath(ws, path)
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

				// Create parent directories if they don't exist
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
		},
		{
			Name:        "append_file",
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
				safePath, err := validatePath(ws, path)
				if err != nil {
					return "", err
				}

				// Create parent directories if they don't exist
				if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
					return "", fmt.Errorf("failed to create directories: %w", err)
				}

				stat, err := os.Stat(safePath)
				if err != nil && !os.IsNotExist(err) {
					return "", fmt.Errorf("error checking file: %w", err)
				}

				if os.IsNotExist(err) {
					// File does not exist: create freshly
					byteContent := []byte(content)
					if err := os.WriteFile(safePath, byteContent, 0644); err != nil {
						return "", fmt.Errorf("failed to create file via append: %w", err)
					}
					lineCount := len(strings.Split(content, "\n"))
					return fmt.Sprintf("file '%s' created successfully via append (%d bytes, %d lines)", path, len(byteContent), lineCount), nil
				}

				// File exists: check line boundary hygiene
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
				// If existing file already had trailing newline and content starts with newline,
				// trim one leading newline to avoid accidental double blank line.
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
		},
		{
			Name:        "list_directory",
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
				safePath, err := validatePath(ws, path)
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
			Name:        "grep_search",
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

				safePath, err := validatePath(ws, searchPath)
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
		},
		{
			Name:        "execute_command",
			Description: "Execute a shell command and return the combined output",
			Interactive: true,
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

				if err := ParseAndValidatePipeline(command, allowedCommands); err != nil {
					return "", err
				}

				execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()

				cmd := exec.CommandContext(execCtx, "bash", "-c", command)
				absRoot, err := filepath.Abs(ws)
				if err != nil {
					absRoot, _ = filepath.Abs(".")
				}
				cmd.Dir = absRoot

				output, err := cmd.CombinedOutput()

				const maxOutputBytes = 100 * 1024
				if len(output) > maxOutputBytes {
					output = append(output[:maxOutputBytes], []byte("\n\n[output truncated: exceeded 100KB limit]")...)
				}

				if err != nil {
					if execCtx.Err() == context.DeadlineExceeded {
						return string(output), fmt.Errorf("command execution timed out after 30s")
					}
					return string(output), fmt.Errorf("command failed: %w", err)
				}

				return string(output), nil
			},
		},
		{
			Name:        "patch_file",
			Description: "Search for a string in a file and replace it with another string",
			Interactive: true,
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

				safePath, err := validatePath(ws, path)
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

				return fmt.Sprintf("file '%s' patched successfully", path), nil
			},
		},
		{
			Name:        "edit_file",
			Description: "Advanced text editing tool supporting regex, line replacement, and insertions",
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

				safePath, err := validatePath(ws, path)
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

				return fmt.Sprintf("file '%s' edited successfully (mode: %s)", path, mode), nil
			},
		},
		{
			Name:        "list_files_recursive",
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
				safePath, err := validatePath(ws, searchPath)
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
		},
		{
			Name:        "search_and_replace",
			Description: "Context-aware search and replace. Searches for a specific block of text and replaces it with another.",
			Interactive: true,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to edit",
					},
					"search_block": map[string]interface{}{
						"type":        "string",
						"description": "The exact block of code/text to search for",
					},
					"replace_block": map[string]interface{}{
						"type":        "string",
						"description": "The new block of code/text to replace it with",
					},
				},
				"required": []string{"path", "search_block", "replace_block"},
			},
			Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
				path, err := getStringArg(args, "path")
				if err != nil {
					return "", err
				}
				search, err := getStringArg(args, "search_block")
				if err != nil {
					return "", err
				}
				replace, err := getStringArg(args, "replace_block")
				if err != nil {
					return "", err
				}

				safePath, err := validatePath(ws, path)
				if err != nil {
					return "", err
				}

				contentBytes, err := os.ReadFile(safePath)
				if err != nil {
					return "", fmt.Errorf("failed to read file: %w", err)
				}
				content := string(contentBytes)

				if !strings.Contains(content, search) {
					return "search block not found. Ensure the block is an exact match (including whitespace/indentation).", nil
				}

				newContent := strings.Replace(content, search, replace, 1)
				err = os.WriteFile(safePath, []byte(newContent), 0644)
				if err != nil {
					return "", fmt.Errorf("failed to write file: %w", err)
				}

				return fmt.Sprintf("file '%s' edited successfully via context matching", path), nil
			},
		},
	}
}

// RegisterDefaultTools adds the default toolset to the manager's registry scoped to workspaceDir
func (m *Manager) RegisterDefaultTools(workspaceDir ...string) {
	ws := m.WorkspaceDir
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
		m.WorkspaceDir = ws
	}
	for _, t := range GetDefaultTools(ws) {
		m.Registry.Register(t)
	}
}
