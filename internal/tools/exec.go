package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/domain"
)

// ExecTools returns the host execution tools.
func ExecTools(workspaceDir string) []Tool {
	return []Tool{
		ExecuteCommandTool(workspaceDir),
	}
}

// DefaultAllowedCommands defines the baseline build and inspection utilities permitted under permissive shell execution.
// Under ADR 011, execute_command is only registered under permissive policy; strict and standard policies omit shell execution entirely.
// Dangerous interpreters (python3, node), network utilities (ssh, curl, wget), and destructive builtins (rm) are excluded from this baseline.
var DefaultAllowedCommands = []string{
	"git", "go", "swift", "make", "cargo", "npm", "ls", "grep", "cat", "echo", "find", "pwd", "date", "mkdir",
	"diff", "wc", "head", "tail", "touch",
}

// sanitizeEnvironment strips sensitive API keys, encryption secrets, and auth tokens from subprocess environments.
func sanitizeEnvironment() []string {
	env := os.Environ()
	sanitized := make([]string, 0, len(env))
	sensitiveKeySubstrings := []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "AUTH", "PLEASE"}

	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 0 {
			continue
		}
		keyUpper := strings.ToUpper(parts[0])

		// Always keep essential system environment variables
		if keyUpper == "PATH" || keyUpper == "HOME" || keyUpper == "USER" || keyUpper == "SHELL" || keyUpper == "LANG" || keyUpper == "TERM" || keyUpper == "TMPDIR" {
			sanitized = append(sanitized, e)
			continue
		}

		// Omit sensitive variables
		isSensitive := false
		for _, s := range sensitiveKeySubstrings {
			if strings.Contains(keyUpper, s) {
				isSensitive = true
				break
			}
		}

		if !isSensitive {
			sanitized = append(sanitized, e)
		}
	}
	return sanitized
}

// ParseAndValidatePipeline decomposes compound shell commands and verifies that every binary is permitted.
func ParseAndValidatePipeline(command string, allowedList []string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("empty command")
	}

	// Permissive mode allows all commands if nil allowedList provided
	if allowedList == nil {
		return nil
	}

	// 1. Forbid dangerous subshell substitutions and privileged escalations
	if strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`") {
		return fmt.Errorf("security error: subshell execution ($(...) or backticks) is prohibited in sandbox")
	}
	if strings.Contains(trimmed, "<(") || strings.Contains(trimmed, ">(") {
		return fmt.Errorf("security error: process substitution (<(...) or >(...)) is prohibited in sandbox")
	}

	// 2. Split pipeline on shell separators: &&, ||, ;, |, and newlines
	normalized := trimmed
	normalized = strings.ReplaceAll(normalized, "&&", "\n")
	normalized = strings.ReplaceAll(normalized, "||", "\n")
	normalized = strings.ReplaceAll(normalized, ";", "\n")
	normalized = strings.ReplaceAll(normalized, "|", "\n")

	lines := strings.Split(normalized, "\n")
	for _, segment := range lines {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}

		parts := strings.Fields(seg)
		if len(parts) == 0 {
			continue
		}

		rawBinary := parts[0]

		// Disallow sudo explicitly
		if rawBinary == "sudo" {
			return fmt.Errorf("security error: sudo is strictly prohibited")
		}

		// Extract base name if full path is used (e.g. /usr/bin/git -> git)
		binary := filepath.Base(rawBinary)

		allowed := false
		for _, a := range allowedList {
			if binary == a {
				allowed = true
				break
			}
		}

		if !allowed {
			return fmt.Errorf("security error: command '%s' is not in the allow-list under active sandbox policy", binary)
		}
	}

	return nil
}

// ExecuteCommandTool constructs the execute_command tool scoped to workspaceDir.
func ExecuteCommandTool(workspaceDir string, allowedList ...[]string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	allowed := DefaultAllowedCommands
	if len(allowedList) > 0 {
		allowed = allowedList[0]
	}

	return Tool{
		Name:        "execute_command",
		Category:    domain.CategoryExecute,
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

			if err := ParseAndValidatePipeline(command, allowed); err != nil {
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
			cmd.Env = append(sanitizeEnvironment(), "PWD="+absRoot)

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
	}
}
