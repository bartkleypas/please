package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
)

// SubagentRunner defines the execution contract for spawning isolated child subagents (ADR 022).
type SubagentRunner interface {
	SpawnSubagent(ctx context.Context, req SubagentRequest) (*SubagentResult, error)
}

// SubagentRequest defines the parameters for a subagent execution.
type SubagentRequest struct {
	ParentSessionID string `json:"parent_session_id"`
	Task            string `json:"task"`
	SessionLabel    string `json:"session_label"`
	ToolPreset      string `json:"tool_preset"`
	IsolateWorktree bool   `json:"isolate_worktree"`
	MaxSteps        int    `json:"max_steps"`
}

// SubagentResult defines the structured observation returned to the parent agent.
type SubagentResult struct {
	Status        string   `json:"status"` // "completed", "budget_exhausted", "failed", "cancelled"
	StepsUsed     int      `json:"steps_used"`
	Summary       string   `json:"summary"`
	Synthesis     string   `json:"synthesis"`
	Branch        string   `json:"branch,omitempty"`
	Commit        string   `json:"commit,omitempty"`
	FilesModified []string `json:"files_modified,omitempty"`
	DiffStat      string   `json:"diff_stat,omitempty"`
}

// SpawnSubagentTool creates the delegation tool bound to runner (ADR 022).
func SpawnSubagentTool(runner SubagentRunner) Tool {
	return Tool{
		Name:        "spawn_subagent",
		Category:    domain.CategoryExecute,
		Description: "Spawns an isolated child subagent to perform a focused investigation, exploratory refactor, or test suite run. The subagent runs in a dedicated workspace and returns a synthesized summary back to you.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task": map[string]interface{}{
					"type":        "string",
					"description": "Clear natural language objective for the subagent.",
				},
				"session_label": map[string]interface{}{
					"type":        "string",
					"description": "Descriptive label for the sub-session (e.g. 'audit-auth-race-conditions').",
				},
				"tool_preset": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"read_only", "full"},
					"default":     "full",
					"description": "Tool permission scope: 'read_only' restricts to inspections; 'full' allows file writes and command executions.",
				},
				"isolate_worktree": map[string]interface{}{
					"type":        "boolean",
					"default":     true,
					"description": "Whether to provision an isolated Git worktree branch. Recommended true for any task modifying files or running builds.",
				},
				"max_steps": map[string]interface{}{
					"type":        "integer",
					"default":     10,
					"description": "Maximum autonomous tool iteration steps (ceiling: 25).",
				},
			},
			"required": []string{"task", "session_label"},
		},
		Interactive: false,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if runner == nil {
				return "", errors.New("subagent runner not configured")
			}

			task, _ := args["task"].(string)
			task = strings.TrimSpace(task)
			if task == "" {
				return "", errors.New("task parameter is required and cannot be empty")
			}

			sessionLabel, _ := args["session_label"].(string)
			sessionLabel = strings.TrimSpace(sessionLabel)
			if sessionLabel == "" {
				return "", errors.New("session_label parameter is required and cannot be empty")
			}

			toolPreset := "full"
			if tp, ok := args["tool_preset"].(string); ok && strings.TrimSpace(tp) != "" {
				tp = strings.ToLower(strings.TrimSpace(tp))
				if tp == "read_only" || tp == "full" {
					toolPreset = tp
				} else {
					return "", fmt.Errorf("invalid tool_preset %q: must be 'read_only' or 'full'", tp)
				}
			}

			isolateWorktree := true
			if iw, ok := args["isolate_worktree"].(bool); ok {
				isolateWorktree = iw
			}

			maxSteps := 10
			if ms, ok := args["max_steps"].(float64); ok && ms > 0 {
				maxSteps = int(ms)
			} else if ms, ok := args["max_steps"].(int); ok && ms > 0 {
				maxSteps = ms
			}
			if maxSteps > 25 {
				maxSteps = 25
			}
			if maxSteps <= 0 {
				maxSteps = 10
			}

			parentSessionID, _ := MemoryContextFromContext(ctx)
			if parentSessionID == "" {
				parentSessionID = "main"
			}

			req := SubagentRequest{
				ParentSessionID: parentSessionID,
				Task:            task,
				SessionLabel:    sessionLabel,
				ToolPreset:      toolPreset,
				IsolateWorktree: isolateWorktree,
				MaxSteps:        maxSteps,
			}

			result, err := runner.SpawnSubagent(ctx, req)
			if err != nil {
				return "", fmt.Errorf("subagent failed: %w", err)
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to format subagent result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}
