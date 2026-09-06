package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// ExecuteCommandTool constructs the execute_command tool scoped to workspaceDir.
func ExecuteCommandTool(workspaceDir string, allowedList ...[]string) Tool {
	ws := workspaceDir
	if ws == "" {
		ws = "."
	}

	allowed := StandardAllowedCommands
	if len(allowedList) > 0 {
		allowed = allowedList[0]
	}

	return Tool{
		Name:        "execute_command",
		Category:    CategoryExecute,
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
