package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type mockSubagentRunner struct {
	lastReq *SubagentRequest
	result  *SubagentResult
	err     error
}

func (m *mockSubagentRunner) SpawnSubagent(ctx context.Context, req SubagentRequest) (*SubagentResult, error) {
	m.lastReq = &req
	return m.result, m.err
}

func TestSpawnSubagentTool_Validation(t *testing.T) {
	runner := &mockSubagentRunner{
		result: &SubagentResult{
			Status:    "completed",
			StepsUsed: 3,
			Summary:   "Found race condition",
			Synthesis: "The test had a race condition in route table.",
		},
	}
	tool := SpawnSubagentTool(runner)

	ctx := context.Background()

	// 1. Missing task
	_, err := tool.Function(ctx, map[string]interface{}{
		"session_label": "audit-task",
	})
	if err == nil || !strings.Contains(err.Error(), "task parameter is required") {
		t.Errorf("expected error for missing task, got: %v", err)
	}

	// 2. Missing session_label
	_, err = tool.Function(ctx, map[string]interface{}{
		"task": "Investigate bug",
	})
	if err == nil || !strings.Contains(err.Error(), "session_label parameter is required") {
		t.Errorf("expected error for missing session_label, got: %v", err)
	}

	// 3. Invalid tool_preset
	_, err = tool.Function(ctx, map[string]interface{}{
		"task":          "Investigate bug",
		"session_label": "audit-task",
		"tool_preset":   "super_admin",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid tool_preset") {
		t.Errorf("expected error for invalid tool_preset, got: %v", err)
	}

	// 4. Default values and max_steps ceiling clamping
	out, err := tool.Function(ctx, map[string]interface{}{
		"task":          "Investigate bug",
		"session_label": "audit-task",
		"max_steps":     100, // Should be clamped to 25
	})
	if err != nil {
		t.Fatalf("unexpected error on valid call: %v", err)
	}

	if runner.lastReq == nil {
		t.Fatal("expected runner to receive request")
	}
	if runner.lastReq.MaxSteps != 25 {
		t.Errorf("expected max_steps clamped to 25, got %d", runner.lastReq.MaxSteps)
	}
	if runner.lastReq.ToolPreset != "full" {
		t.Errorf("expected default tool_preset 'full', got %s", runner.lastReq.ToolPreset)
	}
	if !runner.lastReq.IsolateWorktree {
		t.Errorf("expected default isolate_worktree true")
	}
	if runner.lastReq.ParentSessionID != "main" {
		t.Errorf("expected default ParentSessionID 'main', got %s", runner.lastReq.ParentSessionID)
	}

	var res SubagentResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("expected valid JSON result, got error: %v, raw: %s", err, out)
	}
	if res.Status != "completed" || res.StepsUsed != 3 {
		t.Errorf("unexpected parsed result: %+v", res)
	}
}

func TestSpawnSubagentTool_ParentSessionContext(t *testing.T) {
	runner := &mockSubagentRunner{
		result: &SubagentResult{Status: "completed"},
	}
	tool := SpawnSubagentTool(runner)

	ctx := WithMemoryContext(context.Background(), "session_custom_42", "node_99")
	_, err := tool.Function(ctx, map[string]interface{}{
		"task":          "Run benchmarks",
		"session_label": "bench",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.lastReq.ParentSessionID != "session_custom_42" {
		t.Errorf("expected ParentSessionID 'session_custom_42', got %s", runner.lastReq.ParentSessionID)
	}
}

func TestToolRegistry_SubagentToolOmission(t *testing.T) {
	// Anti-recursion invariant: child tool presets must omit spawn_subagent
	tmpDir := t.TempDir()

	// 1. Full child toolset
	reg := NewToolRegistry()
	for _, tr := range SensoryTools(tmpDir, tmpDir) {
		reg.Register(tr)
	}
	for _, tr := range MutateTools(tmpDir, tmpDir) {
		reg.Register(tr)
	}
	for _, tr := range ExecTools(tmpDir) {
		reg.Register(tr)
	}

	if _, exists := reg.Tools["spawn_subagent"]; exists {
		t.Errorf("spawn_subagent must NOT exist in default child toolset")
	}
	if _, exists := reg.Tools["reconcile_subagent"]; exists {
		t.Errorf("reconcile_subagent must NOT exist in default child toolset")
	}
	if _, exists := reg.Tools["inspect_subagent"]; exists {
		t.Errorf("inspect_subagent must NOT exist in default child toolset")
	}

	// 2. Read-only child toolset
	roReg := NewToolRegistry()
	for _, tr := range SensoryTools(tmpDir, tmpDir) {
		roReg.Register(tr)
	}

	if _, exists := roReg.Tools["spawn_subagent"]; exists {
		t.Errorf("spawn_subagent must NOT exist in read_only child toolset")
	}
	if _, exists := roReg.Tools["reconcile_subagent"]; exists {
		t.Errorf("reconcile_subagent must NOT exist in read_only child toolset")
	}
	if _, exists := roReg.Tools["inspect_subagent"]; exists {
		t.Errorf("inspect_subagent must NOT exist in read_only child toolset")
	}
	if _, exists := roReg.Tools["execute_command"]; exists {
		t.Errorf("execute_command must NOT exist in read_only child toolset")
	}
	if _, exists := roReg.Tools["write_file"]; exists {
		t.Errorf("write_file must NOT exist in read_only child toolset")
	}
}

type mockSubagentReconciler struct {
	lastReq *ReconcileSubagentRequest
	result  *ReconcileSubagentResult
	err     error
}

func (m *mockSubagentReconciler) ReconcileSubagent(ctx context.Context, req ReconcileSubagentRequest) (*ReconcileSubagentResult, error) {
	m.lastReq = &req
	return m.result, m.err
}

type mockSubagentAuditor struct {
	lastReq *InspectSubagentRequest
	result  string
	err     error
}

func (m *mockSubagentAuditor) InspectSubagent(ctx context.Context, req InspectSubagentRequest) (string, error) {
	m.lastReq = &req
	return m.result, m.err
}

func TestReconcileSubagentTool_ValidationAndDispatch(t *testing.T) {
	rec := &mockSubagentReconciler{
		result: &ReconcileSubagentResult{
			Strategy:    "squash",
			Commit:      "abc1234",
			FilesMerged: []string{"main.go"},
			Message:     "Reconciled successfully",
		},
	}
	tool := ReconcileSubagentTool(rec)

	if tool.Category != "execute" {
		t.Errorf("expected CategoryExecute, got %s", tool.Category)
	}

	ctx := context.Background()

	// Missing session_id
	_, err := tool.Function(ctx, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "session_id parameter is required") {
		t.Errorf("expected error for missing session_id, got: %v", err)
	}

	// Valid call with defaults
	out, err := tool.Function(ctx, map[string]interface{}{
		"session_id": "sub_1234",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.lastReq == nil || rec.lastReq.SessionID != "sub_1234" {
		t.Errorf("expected request session_id 'sub_1234', got %+v", rec.lastReq)
	}
	if rec.lastReq.Strategy != "squash" {
		t.Errorf("expected default strategy 'squash', got %s", rec.lastReq.Strategy)
	}
	if !rec.lastReq.CleanupWorktree {
		t.Errorf("expected default cleanup_worktree true")
	}

	var parsed ReconcileSubagentResult
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("failed to parse output JSON: %v", err)
	}
	if parsed.Commit != "abc1234" || len(parsed.FilesMerged) != 1 {
		t.Errorf("unexpected parsed output: %+v", parsed)
	}
}

func TestInspectSubagentTool_ValidationAndDispatch(t *testing.T) {
	auditor := &mockSubagentAuditor{
		result: "=== Subagent Session Summary ===",
	}
	tool := InspectSubagentTool(auditor)

	if tool.Category != "sensory" {
		t.Errorf("expected CategorySensory, got %s", tool.Category)
	}

	ctx := context.Background()

	// Missing session_id
	_, err := tool.Function(ctx, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "session_id parameter is required") {
		t.Errorf("expected error for missing session_id, got: %v", err)
	}

	// Default view
	out, err := tool.Function(ctx, map[string]interface{}{
		"session_id": "sub_5678",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auditor.lastReq == nil || auditor.lastReq.View != "summary" {
		t.Errorf("expected default view 'summary', got %+v", auditor.lastReq)
	}
	if out != "=== Subagent Session Summary ===" {
		t.Errorf("unexpected output: %s", out)
	}

	// Steps view
	_, _ = tool.Function(ctx, map[string]interface{}{
		"session_id": "sub_5678",
		"view":       "steps",
	})
	if auditor.lastReq.View != "steps" {
		t.Errorf("expected view 'steps', got %s", auditor.lastReq.View)
	}
}

func TestSubagentResult_CandidateMemoriesSerialization(t *testing.T) {
	res := SubagentResult{
		Status:    "completed",
		StepsUsed: 2,
		Summary:   "Done",
		Synthesis: "Result synthesis",
		CandidateMemories: []CandidateMemory{
			{Key: "db_version", Content: "Postgres 16", Scope: "workspace"},
		},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !strings.Contains(string(data), "candidate_memories") || !strings.Contains(string(data), "Postgres 16") {
		t.Errorf("expected candidate_memories in JSON: %s", string(data))
	}
}

