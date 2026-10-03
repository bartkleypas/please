package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
	"github.com/bartkleypas/please/internal/worktree"
	"github.com/google/uuid"
)

// SubagentOrchestrator coordinates child subagent execution, worktree sandboxing,
// and synthesized perception returns (ADR 022).
type SubagentOrchestrator struct {
	Manager  *Manager
	Provider providers.Provider
	Config   *config.Config
}

// NewSubagentOrchestrator creates a SubagentOrchestrator instance.
func NewSubagentOrchestrator(mgr *Manager, provider providers.Provider, cfg *config.Config) *SubagentOrchestrator {
	return &SubagentOrchestrator{
		Manager:  mgr,
		Provider: provider,
		Config:   cfg,
	}
}

// SpawnSubagent executes an isolated child agent session in a dedicated worktree (ADR 022).
func (o *SubagentOrchestrator) SpawnSubagent(ctx context.Context, req tools.SubagentRequest) (*tools.SubagentResult, error) {
	if o.Manager == nil {
		return nil, errors.New("manager not configured")
	}
	if o.Provider == nil {
		return nil, errors.New("provider not configured")
	}

	subID := fmt.Sprintf("sub_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
	childWorkspace := o.Manager.WorkspaceDir
	branchName := ""
	baseCommit := ""
	var wtMgr *worktree.Manager
	isWorktree := false

	if req.IsolateWorktree {
		configDir, _ := config.GetConfigDir()
		wtMgr = worktree.NewManager(configDir, o.Manager.WorkspaceDir)
		if wtMgr.IsGitAvailable() && wtMgr.IsGitRepo() {
			branch := fmt.Sprintf("subsession/%s", subID)
			wtDir, actBranch, err := wtMgr.EnsureWorktreeBranch(subID, branch)
			if err == nil && wtDir != "" {
				childWorkspace = wtDir
				branchName = actBranch
				isWorktree = true
				baseCommit, _ = wtMgr.GetHeadCommit(childWorkspace)
			}
		}
	}

	// Clone manager scoped to child workspace
	childMgr := o.Manager.CloneWithWorkspace(childWorkspace, o.Manager.WorkspaceDir)

	// Build child tool registry strictly according to tool_preset.
	// Anti-recursion guard: spawn_subagent is NEVER registered in childMgr.Registry.
	childMgr.Registry = tools.NewToolRegistry()
	for _, t := range tools.SensoryTools(childWorkspace, o.Manager.WorkspaceDir) {
		childMgr.Registry.Register(t)
	}
	if req.ToolPreset != "read_only" {
		for _, t := range tools.MutateTools(childWorkspace, o.Manager.WorkspaceDir) {
			childMgr.Registry.Register(t)
		}
		for _, t := range tools.ExecTools(childWorkspace) {
			childMgr.Registry.Register(t)
		}
	}

	// Memory scoping: read workspace knowledge, isolate writes to child session
	if memStore, ok := childMgr.Storage.(storage.MemoryStore); ok && memStore != nil {
		childMgr.Registry.RegisterMemory(NewMemoryToolsAdapter(memStore), "session")
	}

	// Observation store for receipt inspection
	var obsStore tools.ObservationStore
	if s, ok := childMgr.Storage.(tools.ObservationStore); ok {
		obsStore = s
	}
	childMgr.Registry.RegisterObservationStore(obsStore, func(receiptID string) (string, error) {
		return childMgr.lookupLegacyObservation(receiptID)
	})

	childHarness := NewSubagentHarness(childMgr, o.Provider, o.Config)

	// Execute single turn bounded by max_steps
	turnReq := TurnRequest{
		SessionID:    subID,
		Message:      req.Task,
		Role:         string(domain.RoleUser),
		MaxToolDepth: req.MaxSteps,
		Context: map[string]string{
			"parent_session_id": req.ParentSessionID,
			"session_label":     req.SessionLabel,
			"subagent":          "true",
		},
	}

	eventCh := make(chan HarnessEvent, 64)
	stepsUsed := 0
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for ev := range eventCh {
			if ev.Kind == HarnessEventToolCall {
				stepsUsed++
			}
		}
	}()

	asstNode, err := childHarness.ExecuteTurn(ctx, turnReq, eventCh)
	close(eventCh)
	<-doneCh

	// If step budget was reached and model did not finalize synthesis, trigger forced synthesis step
	if err == nil && asstNode != nil && (asstNode.Content == "" || len(asstNode.ToolCalls) > 0) && stepsUsed >= req.MaxSteps {
		synthReq := TurnRequest{
			SessionID:    subID,
			ParentID:     asstNode.ID,
			Message:      "Step budget reached. Synthesize your current findings, diagnostic progress, and conclusions.",
			Role:         string(domain.RoleUser),
			MaxToolDepth: 1,
		}
		synthEventCh := make(chan HarnessEvent, 16)
		synthDone := make(chan struct{})
		go func() {
			defer close(synthDone)
			for range synthEventCh {
			}
		}()
		synthNode, synthErr := childHarness.ExecuteTurn(ctx, synthReq, synthEventCh)
		close(synthEventCh)
		<-synthDone
		if synthErr == nil && synthNode != nil {
			asstNode = synthNode
		}
	}

	status := "completed"
	if err != nil {
		status = "failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "cancelled"
		}
	} else if stepsUsed >= req.MaxSteps {
		status = "budget_exhausted"
	}

	synthesis := ""
	if asstNode != nil {
		synthesis = asstNode.Content
	}

	summary := ""
	for _, line := range strings.Split(strings.TrimSpace(synthesis), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			summary = line
			break
		}
	}
	if len(summary) > 120 {
		summary = summary[:117] + "..."
	}

	var filesModified []string
	diffStat := ""
	commitHash := ""

	if isWorktree && wtMgr != nil {
		hasChanges, modFiles, _ := wtMgr.InspectChanges(childWorkspace, baseCommit)
		filesModified = modFiles
		commitHash, _ = wtMgr.GetHeadCommit(childWorkspace)
		diffStat, _ = wtMgr.GetDiffStat(childWorkspace, baseCommit)

		// Read-Only / Clean Tasks: If no commits and no modifications, or if read_only preset, prune worktree
		if (!hasChanges && commitHash == baseCommit) || req.ToolPreset == "read_only" {
			_ = wtMgr.RemoveWorktree(subID, true, true)
			branchName = ""
		}
	}

	return &tools.SubagentResult{
		Status:        status,
		StepsUsed:     stepsUsed,
		Summary:       summary,
		Synthesis:     synthesis,
		Branch:        branchName,
		Commit:        commitHash,
		FilesModified: filesModified,
		DiffStat:      diffStat,
	}, nil
}
