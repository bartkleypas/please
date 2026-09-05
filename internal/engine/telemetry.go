package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DeriveAmbientTelemetry constructs a bounded, low-entropy key-value telemetry string.
// Captures workspace directory, git branch, local timestamp, and optional client context
// (e.g. active file focus and cursor line). Strictly bounded in size (<256 bytes).
func DeriveAmbientTelemetry(workspaceDir string, clientContext map[string]string) string {
	var lines []string

	// 1. Current working directory (normalized workspace path)
	if workspaceDir != "" {
		if abs, err := filepath.Abs(workspaceDir); err == nil {
			lines = append(lines, fmt.Sprintf("cwd: %s", abs))
		} else {
			lines = append(lines, fmt.Sprintf("cwd: %s", workspaceDir))
		}
	}

	// 2. Active Git branch (fast, non-blocking check with timeout)
	if branch := getGitBranch(workspaceDir); branch != "" {
		lines = append(lines, fmt.Sprintf("git_branch: %s", branch))
	}

	// 3. Local timestamp (RFC 3339 with local timezone offset)
	lines = append(lines, fmt.Sprintf("local_time: %s", time.Now().Format("2006-01-02T15:04:05-07:00")))

	// 4. Workspace Index file (fast stat check for canonical entry point)
	if indexFile := findWorkspaceIndex(workspaceDir); indexFile != "" {
		lines = append(lines, fmt.Sprintf("index_file: %s", indexFile))
	}

	// 5. Optional client editor telemetry (if provided)
	if clientContext != nil {
		if activeFile, ok := clientContext["active_file"]; ok && activeFile != "" {
			lines = append(lines, fmt.Sprintf("active_file: %s", activeFile))
		}
		if cursorLine, ok := clientContext["cursor_line"]; ok && cursorLine != "" {
			lines = append(lines, fmt.Sprintf("cursor_line: %s", cursorLine))
		}
	}

	return strings.Join(lines, "\n")
}

// FormatTelemetryEnvelope wraps the user prompt and peripheral telemetry in isolated XML tags.
// If telemetry is empty, it returns the prompt wrapped only in <USER_REQUEST>.
func FormatTelemetryEnvelope(userPrompt string, telemetryData string) string {
	trimmedPrompt := strings.TrimSpace(userPrompt)
	trimmedTelem := strings.TrimSpace(telemetryData)

	if trimmedTelem == "" {
		return fmt.Sprintf("<USER_REQUEST>\n%s\n</USER_REQUEST>", trimmedPrompt)
	}

	return fmt.Sprintf("<USER_REQUEST>\n%s\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\n%s\n</ADDITIONAL_METADATA>", trimmedPrompt, trimmedTelem)
}

// getGitBranch executes `git rev-parse --abbrev-ref HEAD` with a 200ms timeout.
// Returns an empty string if not a git repo, git is not installed, or command times out.
func getGitBranch(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if dir != "" {
		cmd.Dir = dir
	}

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" || branch == "" {
		return ""
	}
	return branch
}

// findWorkspaceIndex checks for the presence of canonical orientation documents.
// Checks candidates in order: index.md, README.md, INDEX.md, readme.md.
func findWorkspaceIndex(dir string) string {
	if dir == "" {
		return ""
	}
	candidates := []string{"index.md", "README.md", "INDEX.md", "readme.md"}
	for _, candidate := range candidates {
		path := filepath.Join(dir, candidate)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}
