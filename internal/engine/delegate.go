package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
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
			"session_id":        subID,
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

	var candidateMemories []tools.CandidateMemory
	if memStore, ok := o.Manager.Storage.(storage.MemoryStore); ok && memStore != nil {
		if mems, err := memStore.QueryMemories(storage.MemoryFilter{SessionID: subID}); err == nil {
			for _, m := range mems {
				candidateMemories = append(candidateMemories, tools.CandidateMemory{
					Key:     m.Key,
					Content: m.Content,
					Scope:   string(m.Scope),
				})
			}
		}
	}

	return &tools.SubagentResult{
		Status:            status,
		StepsUsed:         stepsUsed,
		Summary:           summary,
		Synthesis:         synthesis,
		Branch:            branchName,
		Commit:            commitHash,
		FilesModified:     filesModified,
		DiffStat:          diffStat,
		CandidateMemories: candidateMemories,
	}, nil
}

// ReconcileSubagent merges, squashes, or discards an isolated subagent worktree branch.
func (o *SubagentOrchestrator) ReconcileSubagent(ctx context.Context, req tools.ReconcileSubagentRequest) (*tools.ReconcileSubagentResult, error) {
	if o.Manager == nil {
		return nil, errors.New("manager not configured")
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, errors.New("session_id parameter is required and cannot be empty")
	}

	branch := sessionID
	if !strings.HasPrefix(branch, "subsession/") && !strings.HasPrefix(branch, "please/") {
		branch = fmt.Sprintf("subsession/%s", sessionID)
	}

	configDir, _ := config.GetConfigDir()
	wtMgr := worktree.NewManager(configDir, o.Manager.WorkspaceDir)

	strategy := req.Strategy
	if strategy == "" {
		strategy = "squash"
	}

	res, err := wtMgr.ReconcileBranch(branch, strategy, req.CleanupWorktree, sessionID)
	if err != nil {
		return nil, err
	}

	return &tools.ReconcileSubagentResult{
		Strategy:    res.Strategy,
		Commit:      res.Commit,
		FilesMerged: res.FilesMerged,
		Message:     res.Message,
	}, nil
}

// InspectSubagent provides forensic inspection of a subagent's execution telemetry or git diff.
func (o *SubagentOrchestrator) InspectSubagent(ctx context.Context, req tools.InspectSubagentRequest) (string, error) {
	if o.Manager == nil {
		return "", errors.New("manager not configured")
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return "", errors.New("session_id parameter is required and cannot be empty")
	}
	cleanSessionID := strings.TrimPrefix(sessionID, "subsession/")

	view := strings.ToLower(strings.TrimSpace(req.View))
	if view == "" {
		view = "summary"
	}

	configDir, _ := config.GetConfigDir()
	wtMgr := worktree.NewManager(configDir, o.Manager.WorkspaceDir)

	switch view {
	case "diff":
		branch := fmt.Sprintf("subsession/%s", cleanSessionID)
		diff, err := wtMgr.GetBranchDiff(branch)
		if err != nil || strings.TrimSpace(diff) == "" {
			wtDir, _ := wtMgr.GetWorktreeDir(cleanSessionID)
			if wtDir != "" {
				diff, _ = wtMgr.GetDiff(wtDir, "")
			}
		}
		if strings.TrimSpace(diff) == "" {
			return fmt.Sprintf("No git diff recorded for subagent session %s (working tree clean, no modifications, or branch already reconciled/discarded).", sessionID), nil
		}
		return diff, nil

	case "steps":
		nodes := o.resolveSubagentNodes(cleanSessionID)
		var steps []toolStepRecord
		for _, n := range nodes {
			if len(n.ToolCalls) > 0 {
				obsMap := make(map[string]domain.ToolObservation)
				for _, obs := range n.Observations {
					obsMap[obs.ToolCallID] = obs
				}
				for idx, tc := range n.ToolCalls {
					obs, ok := obsMap[tc.ID]
					if !ok && idx < len(n.Observations) {
						obs = n.Observations[idx]
					}
					status := "OK"
					if obs.Error != "" {
						status = "Error"
					}
					steps = append(steps, toolStepRecord{
						Step:      len(steps) + 1,
						Tool:      tc.Function.Name,
						Arguments: string(tc.Function.Arguments),
						Status:    status,
						Error:     obs.Error,
					})
				}
			}
		}

		if len(steps) == 0 {
			return fmt.Sprintf("No tool execution steps recorded for session %s.", sessionID), nil
		}

		var b strings.Builder
		b.WriteString(fmt.Sprintf("=== Subagent Execution Steps: %s ===\n\n", sessionID))
		b.WriteString(formatStepsTable(steps))

		var errTraces []string
		for _, s := range steps {
			if s.Error != "" {
				errTraces = append(errTraces, fmt.Sprintf("[Step %d - %s]: %s", s.Step, s.Tool, s.Error))
			}
		}
		if len(errTraces) > 0 {
			b.WriteString("\n--- Error Traces ---\n")
			b.WriteString(strings.Join(errTraces, "\n\n"))
			b.WriteString("\n")
		}

		return b.String(), nil

	case "summary":
		fallthrough
	default:
		nodes := o.resolveSubagentNodes(cleanSessionID)
		var task string
		var synthesis string
		var status = "completed"
		toolCallCount := 0
		hasError := false

		for _, n := range nodes {
			if n.Role == domain.RoleUser && task == "" {
				task = n.Content
			}
			if n.Role == domain.RoleAssistant {
				synthesis = n.Content
				toolCallCount += len(n.ToolCalls)
				for _, obs := range n.Observations {
					if obs.Error != "" {
						hasError = true
					}
				}
			}
		}
		if hasError {
			status = "completed_with_errors"
		}
		if len(nodes) == 0 {
			status = "unknown_or_pruned"
		}

		var candidateMems []string
		if memStore, ok := o.Manager.Storage.(storage.MemoryStore); ok && memStore != nil {
			if mems, err := memStore.QueryMemories(storage.MemoryFilter{SessionID: cleanSessionID}); err == nil {
				for _, m := range mems {
					candidateMems = append(candidateMems, fmt.Sprintf("- [%s] %s: %s", m.Scope, m.Key, m.Content))
				}
			}
		}

		branch := fmt.Sprintf("subsession/%s", cleanSessionID)
		diffStat := ""
		wtDir, _ := wtMgr.GetWorktreeDir(cleanSessionID)
		if wtDir != "" {
			diffStat, _ = wtMgr.GetDiffStat(wtDir, "")
		}
		if diffStat == "" {
			diffStat, _ = wtMgr.GetBranchDiff(branch)
		}

		var b strings.Builder
		b.WriteString(fmt.Sprintf("=== Subagent Session Summary: %s ===\n", sessionID))
		b.WriteString(fmt.Sprintf("Status:       %s\n", status))
		b.WriteString(fmt.Sprintf("Total Steps:  %d\n", toolCallCount))
		if task != "" {
			taskPreview := task
			if len(taskPreview) > 80 {
				taskPreview = taskPreview[:77] + "..."
			}
			b.WriteString(fmt.Sprintf("Task:         %s\n", taskPreview))
		}
		if diffStat != "" {
			b.WriteString(fmt.Sprintf("Diff Stat:\n%s\n", diffStat))
		}
		if len(candidateMems) > 0 {
			b.WriteString(fmt.Sprintf("Candidate Memories (%d):\n%s\n", len(candidateMems), strings.Join(candidateMems, "\n")))
		}
		if synthesis != "" {
			synthPreview := strings.TrimSpace(synthesis)
			if len(synthPreview) > 300 {
				synthPreview = synthPreview[:297] + "..."
			}
			b.WriteString(fmt.Sprintf("Synthesis:\n%s\n", synthPreview))
		}

		return b.String(), nil
	}
}

type toolStepRecord struct {
	Step      int
	Tool      string
	Arguments string
	Status    string
	Error     string
}

func (o *SubagentOrchestrator) resolveSubagentNodes(sessionID string) []*graph.Node {
	var sessionNodes []*graph.Node

	if o.Manager.Storage != nil {
		if headID, err := o.Manager.Storage.GetSessionHead(sessionID); err == nil && headID != "" {
			if o.Manager.Graph != nil {
				if path, err := o.Manager.Graph.GetPath(headID); err == nil {
					sessionNodes = path
				}
			}
			if len(sessionNodes) == 0 && o.Manager != nil {
				currID := headID
				var revNodes []*graph.Node
				visited := make(map[string]bool)
				for currID != "" && !visited[currID] {
					visited[currID] = true
					currNode, _ := o.Manager.GetNode(currID)
					if currNode == nil {
						break
					}
					revNodes = append(revNodes, currNode)
					currID = currNode.ParentID
				}
				for i := len(revNodes) - 1; i >= 0; i-- {
					sessionNodes = append(sessionNodes, revNodes[i])
				}
			}
		}
	}

	if len(sessionNodes) == 0 && o.Manager.Graph != nil {
		for _, n := range o.Manager.Graph.GetAllNodes() {
			if n.Metadata != nil && (n.Metadata["session_id"] == sessionID || strings.Contains(n.Metadata["session_label"], sessionID)) {
				sessionNodes = append(sessionNodes, n)
			}
		}
	}

	var childNodes []*graph.Node
	foundStart := false
	for _, n := range sessionNodes {
		if !foundStart {
			if n.Metadata != nil && (n.Metadata["session_id"] == sessionID || n.Metadata["subagent"] == "true") {
				foundStart = true
				childNodes = append(childNodes, n)
			}
		} else {
			childNodes = append(childNodes, n)
		}
	}
	if len(childNodes) == 0 {
		return sessionNodes
	}
	return childNodes
}

func formatStepsTable(steps []toolStepRecord) string {
	maxToolLen := 4 // "Tool"
	maxArgsLen := 9 // "Arguments"
	for _, s := range steps {
		if len(s.Tool) > maxToolLen {
			maxToolLen = len(s.Tool)
		}
		args := s.Arguments
		if len(args) > 50 {
			args = args[:47] + "..."
		}
		if len(args) > maxArgsLen {
			maxArgsLen = len(args)
		}
	}
	if maxToolLen > 24 {
		maxToolLen = 24
	}
	if maxArgsLen > 50 {
		maxArgsLen = 50
	}

	lineSep := fmt.Sprintf("+------+-%s-+-%s-+--------+", strings.Repeat("-", maxToolLen), strings.Repeat("-", maxArgsLen))
	header := fmt.Sprintf("| %-4s | %-*s | %-*s | %-6s |", "Step", maxToolLen, "Tool", maxArgsLen, "Arguments", "Status")

	var sb strings.Builder
	sb.WriteString(lineSep + "\n")
	sb.WriteString(header + "\n")
	sb.WriteString(lineSep + "\n")
	for _, s := range steps {
		toolStr := s.Tool
		if len(toolStr) > maxToolLen {
			toolStr = toolStr[:maxToolLen-3] + "..."
		}
		argStr := s.Arguments
		argStr = strings.ReplaceAll(argStr, "\n", " ")
		if len(argStr) > maxArgsLen {
			argStr = argStr[:maxArgsLen-3] + "..."
		}
		row := fmt.Sprintf("| %-4d | %-*s | %-*s | %-6s |", s.Step, maxToolLen, toolStr, maxArgsLen, argStr, s.Status)
		sb.WriteString(row + "\n")
	}
	sb.WriteString(lineSep + "\n")
	return sb.String()
}
