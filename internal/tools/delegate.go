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
	TargetDir       string `json:"target_dir,omitempty"`
	ToolPreset      string `json:"tool_preset"`
	IsolateWorktree bool   `json:"isolate_worktree"`
	MaxSteps        int    `json:"max_steps"`
}

// CandidateMemory defines an atomic piece of knowledge discovered by a child subagent.
type CandidateMemory struct {
	Key     string `json:"key"`
	Content string `json:"content"`
	Scope   string `json:"scope"`
}

// SubagentResult defines the structured observation returned to the parent agent.
type SubagentResult struct {
	Status            string            `json:"status"` // "completed", "budget_exhausted", "failed", "cancelled"
	StepsUsed         int               `json:"steps_used"`
	Summary           string            `json:"summary"`
	Synthesis         string            `json:"synthesis"`
	Branch            string            `json:"branch,omitempty"`
	Commit            string            `json:"commit,omitempty"`
	RepoPath          string            `json:"repo_path,omitempty"`
	FilesModified     []string          `json:"files_modified,omitempty"`
	DiffStat          string            `json:"diff_stat,omitempty"`
	CandidateMemories []CandidateMemory `json:"candidate_memories,omitempty"`
}

// SubagentReconciler defines the contract for reconciling an isolated subagent's changes.
type SubagentReconciler interface {
	ReconcileSubagent(ctx context.Context, req ReconcileSubagentRequest) (*ReconcileSubagentResult, error)
}

// SubagentAuditor defines the contract for forensically inspecting a subagent session.
type SubagentAuditor interface {
	InspectSubagent(ctx context.Context, req InspectSubagentRequest) (string, error)
}

// ReconcileSubagentRequest defines the parameters for subagent reconciliation.
type ReconcileSubagentRequest struct {
	SessionID       string `json:"session_id"`
	TargetDir       string `json:"target_dir,omitempty"`
	Strategy        string `json:"strategy"`
	CleanupWorktree bool   `json:"cleanup_worktree"`
}

// ReconcileSubagentResult defines the structured result of subagent reconciliation.
type ReconcileSubagentResult struct {
	Strategy    string   `json:"strategy"`
	Commit      string   `json:"commit,omitempty"`
	FilesMerged []string `json:"files_merged,omitempty"`
	Message     string   `json:"message,omitempty"`
}

// InspectSubagentRequest defines the parameters for inspecting a subagent session.
type InspectSubagentRequest struct {
	SessionID string `json:"session_id"`
	View      string `json:"view"` // "summary", "steps", "diff"
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
				"target_dir": map[string]interface{}{
					"type":        "string",
					"description": "Optional subproject directory or nested repository relative to current workspace to target for worktree isolation. Defaults to workspace root.",
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

			targetDir, _ := args["target_dir"].(string)
			targetDir = strings.TrimSpace(targetDir)

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
				TargetDir:       targetDir,
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

// ReconcileSubagentTool creates the reconciliation tool bound to reconciler.
func ReconcileSubagentTool(reconciler SubagentReconciler) Tool {
	return Tool{
		Name:        "reconcile_subagent",
		Category:    domain.CategoryExecute,
		Description: "Reconciles an isolated subagent branch into the primary workspace using squash, merge, cherry_pick, or discard. Requires confirmation in standard sandbox policy.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Identifier of the subagent session to reconcile (e.g. 'sub_1234abcd').",
				},
				"target_dir": map[string]interface{}{
					"type":        "string",
					"description": "Optional subproject directory or nested repository relative to current workspace where the worktree branch was isolated.",
				},
				"strategy": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"squash", "merge", "cherry_pick", "discard"},
					"default":     "squash",
					"description": "Reconciliation strategy: 'squash' (squash commits into one), 'merge' (merge subsession branch), 'cherry_pick' (cherry-pick latest commit), or 'discard' (drop branch and clean up worktree).",
				},
				"cleanup_worktree": map[string]interface{}{
					"type":        "boolean",
					"default":     true,
					"description": "Whether to remove the isolated worktree directory and branch after reconciliation.",
				},
			},
			"required": []string{"session_id"},
		},
		Interactive: false,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if reconciler == nil {
				return "", errors.New("subagent reconciler not configured")
			}

			sessionID, _ := args["session_id"].(string)
			sessionID = strings.TrimSpace(sessionID)
			if sessionID == "" {
				return "", errors.New("session_id parameter is required and cannot be empty")
			}

			targetDir, _ := args["target_dir"].(string)
			targetDir = strings.TrimSpace(targetDir)

			strategy := "squash"
			if s, ok := args["strategy"].(string); ok && strings.TrimSpace(s) != "" {
				strategy = strings.ToLower(strings.TrimSpace(s))
			}

			cleanupWorktree := true
			if cw, ok := args["cleanup_worktree"].(bool); ok {
				cleanupWorktree = cw
			}

			req := ReconcileSubagentRequest{
				SessionID:       sessionID,
				TargetDir:       targetDir,
				Strategy:        strategy,
				CleanupWorktree: cleanupWorktree,
			}

			res, err := reconciler.ReconcileSubagent(ctx, req)
			if err != nil {
				return "", fmt.Errorf("reconciliation failed: %w", err)
			}

			jsonBytes, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to format reconciliation result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}

// InspectSubagentTool creates the forensic inspection tool bound to auditor.
func InspectSubagentTool(auditor SubagentAuditor) Tool {
	return Tool{
		Name:        "inspect_subagent",
		Category:    domain.CategorySensory,
		Description: "Forensically inspects a child subagent's execution telemetry, step-by-step tool traces, or git diff.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Identifier of the subagent session to inspect.",
				},
				"view": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"summary", "steps", "diff"},
					"default":     "summary",
					"description": "Inspection view: 'summary' (status, steps, modified files), 'steps' (tabular step execution trace and errors), or 'diff' (git diff of branch changes).",
				},
			},
			"required": []string{"session_id"},
		},
		Interactive: false,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if auditor == nil {
				return "", errors.New("subagent auditor not configured")
			}

			sessionID, _ := args["session_id"].(string)
			sessionID = strings.TrimSpace(sessionID)
			if sessionID == "" {
				return "", errors.New("session_id parameter is required and cannot be empty")
			}

			view := "summary"
			if v, ok := args["view"].(string); ok && strings.TrimSpace(v) != "" {
				view = strings.ToLower(strings.TrimSpace(v))
			}

			req := InspectSubagentRequest{
				SessionID: sessionID,
				View:      view,
			}

			return auditor.InspectSubagent(ctx, req)
		},
	}
}
