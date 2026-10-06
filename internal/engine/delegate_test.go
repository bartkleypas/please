package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
	"github.com/bartkleypas/please/internal/worktree"
)

func TestSubagentOrchestrator_ReadOnlyWorktreePruned(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := t.TempDir() + "/vault.db"
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			// Subagent reads file and concludes
			for _, m := range messages {
				if m.Role == domain.RoleTool {
					return "Audit complete. No issues found.", "Concluded", nil, nil
				}
			}
			return "", "Reading file", []domain.ToolCall{
				{
					ID:   "call_ro_1",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path": "math.go"}`),
					},
				},
			}, nil
		},
	}

	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)
	res, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Audit math.go",
		SessionLabel:    "read-only-audit",
		ToolPreset:      "read_only",
		IsolateWorktree: true,
		MaxSteps:        5,
	})
	if err != nil {
		t.Fatalf("SpawnSubagent failed: %v", err)
	}

	if res.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", res.Status)
	}
	// For read-only tasks, worktree and branch should be pruned
	if res.Branch != "" {
		t.Errorf("expected pruned branch for read-only preset, got %s", res.Branch)
	}
}

func TestSubagentOrchestrator_BudgetExhaustion(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := t.TempDir() + "/vault.db"
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	var mu sync.Mutex
	callCount := 0

	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			mu.Lock()
			defer mu.Unlock()
			callCount++

			// Check if this is the forced synthesis prompt
			for _, m := range messages {
				if strings.Contains(m.Content, "Step budget reached") {
					return "Final synthesis: Hit step budget while researching.", "Finished", nil, nil
				}
			}

			// Keep calling read_file to exhaust budget
			return "", "Still reading", []domain.ToolCall{
				{
					ID:   "call_loop",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path": "math.go"}`),
					},
				},
			}, nil
		},
	}

	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)
	res, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Explore codebase endlessly",
		SessionLabel:    "budget-test",
		ToolPreset:      "read_only",
		IsolateWorktree: false,
		MaxSteps:        2,
	})
	if err != nil {
		t.Fatalf("SpawnSubagent failed: %v", err)
	}

	if res.Status != "budget_exhausted" {
		t.Errorf("expected status 'budget_exhausted', got %s", res.Status)
	}
	if res.StepsUsed != 2 {
		t.Errorf("expected 2 steps used, got %d", res.StepsUsed)
	}
	if !strings.Contains(res.Synthesis, "Hit step budget") {
		t.Errorf("expected forced synthesis result, got: %s", res.Synthesis)
	}
}

func TestSubagentOrchestrator_Cancellation(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := t.TempDir() + "/vault.db"
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	ctx, cancel := context.WithCancel(context.Background())

	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			cancel() // Cancel during first call
			<-time.After(10 * time.Millisecond)
			return "", "", nil, context.Canceled
		},
	}

	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)
	res, _ := orchestrator.SpawnSubagent(ctx, tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Will be cancelled",
		SessionLabel:    "cancel-test",
		ToolPreset:      "read_only",
		IsolateWorktree: false,
		MaxSteps:        5,
	})

	if res != nil && res.Status != "cancelled" && res.Status != "failed" {
		t.Errorf("expected status 'cancelled' or 'failed', got %s", res.Status)
	}
}

func TestSubagentOrchestrator_ReconcileSubagent(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()
	orchestrator := NewSubagentOrchestrator(mgr, nil, cfg)

	wtMgr := worktree.NewManager(configDir, repoDir)
	subID := "sub_orch_rec"
	branch := "subsession/" + subID
	wtDir, _, err := wtMgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	gitPath, _ := exec.LookPath("git")
	changeFile := filepath.Join(wtDir, "orch_change.txt")
	_ = os.WriteFile(changeFile, []byte("subagent work"), 0644)
	exec.Command(gitPath, "-C", wtDir, "add", "orch_change.txt").Run()
	exec.Command(gitPath, "-C", wtDir, "commit", "-m", "subagent commit").Run()

	// 1. Reconcile via squash
	recRes, err := orchestrator.ReconcileSubagent(context.Background(), tools.ReconcileSubagentRequest{
		SessionID:       subID,
		Strategy:        "squash",
		CleanupWorktree: true,
	})
	if err != nil {
		t.Fatalf("ReconcileSubagent failed: %v", err)
	}

	if recRes.Strategy != "squash" || recRes.Commit == "" {
		t.Errorf("unexpected ReconcileSubagent result: %+v", recRes)
	}

	primaryFile := filepath.Join(repoDir, "orch_change.txt")
	if data, err := os.ReadFile(primaryFile); err != nil || string(data) != "subagent work" {
		t.Errorf("file not found in primary after squash: %v", err)
	}

	// 2. Discard strategy
	subID2 := "sub_orch_discard"
	branch2 := "subsession/" + subID2
	wtDir2, _, _ := wtMgr.EnsureWorktreeBranch(subID2, branch2)
	_ = os.WriteFile(filepath.Join(wtDir2, "discard.txt"), []byte("throwaway"), 0644)
	exec.Command(gitPath, "-C", wtDir2, "add", "discard.txt").Run()
	exec.Command(gitPath, "-C", wtDir2, "commit", "-m", "throwaway commit").Run()

	discardRes, err := orchestrator.ReconcileSubagent(context.Background(), tools.ReconcileSubagentRequest{
		SessionID:       subID2,
		Strategy:        "discard",
		CleanupWorktree: true,
	})
	if err != nil {
		t.Fatalf("discard failed: %v", err)
	}
	if discardRes.Strategy != "discard" {
		t.Errorf("expected strategy 'discard', got %s", discardRes.Strategy)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "discard.txt")); !os.IsNotExist(err) {
		t.Errorf("discarded file should not exist in primary workspace")
	}
}

func TestSubagentOrchestrator_InspectSubagent(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()
	orchestrator := NewSubagentOrchestrator(mgr, nil, cfg)

	subID := "sub_inspect_123"

	// Create user node and assistant node representing child subagent session
	userNode := &graph.Node{
		ID:        "node_001_user",
		Role:      domain.RoleUser,
		Content:   "Analyze security policies",
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"session_id": subID,
			"subagent":   "true",
		},
	}
	g.AddNode(userNode)
	_ = sqlStore.SaveNode(userNode)

	asstNode := &graph.Node{
		ID:        "node_002_asst",
		ParentID:  userNode.ID,
		Role:      domain.RoleAssistant,
		Content:   "Security analysis complete: Found 1 vulnerability.",
		Timestamp: time.Now(),
		ToolCalls: []domain.ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path": "math.go"}`),
				},
			},
			{
				ID:   "call_2",
				Type: "function",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      "execute_command",
					Arguments: json.RawMessage(`{"command": "cat /etc/passwd"}`),
				},
			},
		},
		Observations: []domain.ToolObservation{
			{
				ToolCallID: "call_1",
				Result:     "package math",
			},
			{
				ToolCallID: "call_2",
				Result:     "permission denied",
				Error:      "exit status 1: permission denied",
			},
		},
	}
	g.AddNode(asstNode)
	_ = sqlStore.SaveNode(asstNode)
	_ = sqlStore.SaveSessionHead(subID, asstNode.ID)

	// Save candidate memory
	_ = sqlStore.SaveMemory(&storage.Memory{
		ID:        "mem_cand_1",
		Key:       "security_flaw",
		Content:   "math.go lacks integer overflow guard",
		Scope:     storage.ScopeSession,
		SessionID: subID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// 1. Inspect view="steps"
	stepsOut, err := orchestrator.InspectSubagent(context.Background(), tools.InspectSubagentRequest{
		SessionID: subID,
		View:      "steps",
	})
	if err != nil {
		t.Fatalf("InspectSubagent steps failed: %v", err)
	}
	if !strings.Contains(stepsOut, "read_file") || !strings.Contains(stepsOut, "execute_command") {
		t.Errorf("steps output missing tool names: %s", stepsOut)
	}
	if !strings.Contains(stepsOut, "Error Traces") || !strings.Contains(stepsOut, "permission denied") {
		t.Errorf("steps output missing error trace: %s", stepsOut)
	}

	// 2. Inspect view="summary"
	summaryOut, err := orchestrator.InspectSubagent(context.Background(), tools.InspectSubagentRequest{
		SessionID: subID,
		View:      "summary",
	})
	if err != nil {
		t.Fatalf("InspectSubagent summary failed: %v", err)
	}
	if !strings.Contains(summaryOut, "Analyze security policies") {
		t.Errorf("summary output missing task: %s", summaryOut)
	}
	if !strings.Contains(summaryOut, "security_flaw") {
		t.Errorf("summary output missing candidate memory: %s", summaryOut)
	}
	if !strings.Contains(summaryOut, "Total Steps:  2") {
		t.Errorf("summary output missing step count: %s", summaryOut)
	}

	// 3. Inspect view="diff"
	diffOut, err := orchestrator.InspectSubagent(context.Background(), tools.InspectSubagentRequest{
		SessionID: subID,
		View:      "diff",
	})
	if err != nil {
		t.Fatalf("InspectSubagent diff failed: %v", err)
	}
	if !strings.Contains(diffOut, "No git diff recorded") {
		t.Errorf("expected clean/empty diff message, got: %s", diffOut)
	}
}

func TestManager_DefaultAndClonedDelegationTools(t *testing.T) {
	primaryDir := t.TempDir()
	worktreeDir := t.TempDir()

	g := graph.NewGraph()
	mgr := NewManager(g, nil)
	mgr.RegisterDefaultTools(primaryDir)

	for _, expectedTool := range []string{"spawn_subagent", "reconcile_subagent", "inspect_subagent"} {
		if _, exists := mgr.Registry.Tools[expectedTool]; !exists {
			t.Errorf("expected %s in mgr.Registry", expectedTool)
		}
	}

	// Cloned manager
	cloned := mgr.CloneWithWorkspace(worktreeDir, primaryDir)
	for _, expectedTool := range []string{"spawn_subagent", "reconcile_subagent", "inspect_subagent"} {
		if _, exists := cloned.Registry.Tools[expectedTool]; !exists {
			t.Errorf("expected %s in cloned.Registry", expectedTool)
		}
	}
}

func TestSubagentOrchestrator_CandidateMemoriesPromotion(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			for _, m := range messages {
				if m.Role == domain.RoleTool {
					return "Investigation concluded with new architectural fact.", "Concluded", nil, nil
				}
			}
			return "", "Saving memory", []domain.ToolCall{
				{
					ID:   "call_mem_1",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "memory_store",
						Arguments: json.RawMessage(`{"key": "db_arch", "content": "Uses SQLite with WAL mode", "category": "architecture"}`),
					},
				},
			}, nil
		},
	}

	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)
	res, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Investigate database architecture",
		SessionLabel:    "db-investigation",
		ToolPreset:      "full",
		IsolateWorktree: false,
		MaxSteps:        5,
	})
	if err != nil {
		t.Fatalf("SpawnSubagent failed: %v", err)
	}

	if len(res.CandidateMemories) != 1 {
		t.Fatalf("expected 1 candidate memory, got %d", len(res.CandidateMemories))
	}
	if res.CandidateMemories[0].Key != "db_arch" || !strings.Contains(res.CandidateMemories[0].Content, "WAL mode") {
		t.Errorf("unexpected candidate memory: %+v", res.CandidateMemories[0])
	}
}

