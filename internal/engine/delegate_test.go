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

func TestSubagentOrchestrator_RejectsMutatingToolsWithoutWorktree(t *testing.T) {
	mgr := &Manager{}
	mockProvider := &providers.MockLLMProvider{}
	cfg := config.NewDefaultConfig()
	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)

	_, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Modify code directly in workspace",
		SessionLabel:    "unsafe-delegation",
		ToolPreset:      "full",
		IsolateWorktree: false,
		MaxSteps:        5,
	})
	if err == nil || !strings.Contains(err.Error(), "mutating tools (tool_preset: 'full') require an isolated worktree") {
		t.Fatalf("expected error rejecting full tools without worktree isolation, got: %v", err)
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
		ToolPreset:      "read_only",
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

func TestSubagentOrchestrator_TargetDir_PathTraversal(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	g := graph.NewGraph()
	mgr := NewManager(g, nil)
	mgr.WorkspaceDir = repoDir
	cfg := config.NewDefaultConfig()
	mockProvider := &providers.MockLLMProvider{}
	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)

	// Attempt path traversal
	_, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Attack workspace",
		SessionLabel:    "exploit",
		TargetDir:       "../../outside",
		ToolPreset:      "read_only",
		IsolateWorktree: false,
		MaxSteps:        1,
	})
	if err == nil || !strings.Contains(err.Error(), "traverses outside workspace") {
		t.Fatalf("expected error for path traversal, got: %v", err)
	}

	// Reconcile path traversal
	_, err = orchestrator.ReconcileSubagent(context.Background(), tools.ReconcileSubagentRequest{
		SessionID: "sub_fake",
		TargetDir: "../../../etc",
	})
	if err == nil || !strings.Contains(err.Error(), "traverses outside workspace") {
		t.Fatalf("expected error for path traversal in reconcile, got: %v", err)
	}
}

func TestSubagentOrchestrator_TargetDir_DelegationAndReconciliation(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}

	// Create nested git repository inside repoDir
	childDir := filepath.Join(repoDir, "child_repo")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("failed to create child_repo: %v", err)
	}
	exec.Command(gitPath, "-C", childDir, "init", "-b", "main").Run()
	exec.Command(gitPath, "-C", childDir, "config", "user.name", "Scenario Tester").Run()
	exec.Command(gitPath, "-C", childDir, "config", "user.email", "tester@please.dev").Run()
	_ = os.WriteFile(filepath.Join(childDir, "child_init.txt"), []byte("child repo init"), 0644)
	exec.Command(gitPath, "-C", childDir, "add", "child_init.txt").Run()
	exec.Command(gitPath, "-C", childDir, "commit", "-m", "init child").Run()

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	sqlStore, _ := storage.NewSQLiteStorage(dbPath, "")

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	// Provider writes a file in the child repo worktree
	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			for _, m := range messages {
				if m.Role == domain.RoleTool {
					return "Subproject file written successfully.", "Concluded", nil, nil
				}
			}
			return "", "Writing child file", []domain.ToolCall{
				{
					ID:   "call_write_child",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "write_file",
						Arguments: json.RawMessage(`{"path": "child_feature.txt", "content": "child feature content"}`),
					},
				},
			}, nil
		},
	}

	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)
	res, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Implement child feature in child_repo",
		SessionLabel:    "child-delegation",
		TargetDir:       "child_repo",
		ToolPreset:      "full",
		IsolateWorktree: true,
		MaxSteps:        5,
	})
	if err != nil {
		t.Fatalf("SpawnSubagent failed: %v", err)
	}

	if res.RepoPath != "child_repo" {
		t.Errorf("expected RepoPath 'child_repo', got %q", res.RepoPath)
	}
	if res.Branch == "" {
		t.Fatalf("expected non-empty branch for isolate_worktree")
	}

	subID := strings.TrimPrefix(res.Branch, "subsession/")

	// Reconcile via squash without specifying target_dir (should auto-resolve from session metadata)
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

	// Verify child_feature.txt exists in childDir, NOT in parent repoDir
	childFile := filepath.Join(childDir, "child_feature.txt")
	if data, err := os.ReadFile(childFile); err != nil || string(data) != "child feature content" {
		t.Errorf("child_feature.txt missing or incorrect in child_repo: %v", err)
	}
	parentFile := filepath.Join(repoDir, "child_feature.txt")
	if _, err := os.Stat(parentFile); !os.IsNotExist(err) {
		t.Errorf("child_feature.txt must NOT exist in parent workspace root")
	}
}

func TestSubagentOrchestrator_InheritsParentBranchGenesisRoot(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	sqlStore, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	// 1. Establish Root A (older system prompt)
	rootA, err := mgr.CreateNode("", domain.RoleSystem, "You are Agent Alpha (Legacy Branch)", false)
	if err != nil {
		t.Fatalf("failed to create rootA: %v", err)
	}
	// Ensure rootA has an earlier timestamp
	rootA.Timestamp = time.Now().Add(-2 * time.Hour)
	_ = sqlStore.SaveNode(rootA)

	// Small pause so UUIDv7 timestamp progresses
	time.Sleep(10 * time.Millisecond)

	// 2. Establish Root B (newer system prompt for active branch)
	rootB, err := mgr.CreateNode("", domain.RoleSystem, "You are George the Archivist (Active Branch)", false)
	if err != nil {
		t.Fatalf("failed to create rootB: %v", err)
	}
	rootB.Timestamp = time.Now().Add(-1 * time.Hour)
	_ = sqlStore.SaveNode(rootB)

	// Re-sync graph to ensure roots ordering
	_, _, _ = mgr.Sync()

	// Verify that the naive GetSystemRoot() returns rootA (the oldest system root)
	sysRoot, err := g.GetSystemRoot()
	if err != nil || sysRoot.ID != rootA.ID {
		t.Fatalf("expected GetSystemRoot to return oldest rootA, got %v (err: %v)", sysRoot, err)
	}

	// 3. Start a parent session under Root B
	time.Sleep(10 * time.Millisecond)
	parentSessionID := "session_branch_b"
	userB, err := mgr.CreateNode(rootB.ID, domain.RoleUser, "Investigate the active branch architecture", false)
	if err != nil {
		t.Fatalf("failed to create userB: %v", err)
	}
	userB.Metadata["session_id"] = parentSessionID
	_ = sqlStore.SaveNode(userB)

	time.Sleep(10 * time.Millisecond)
	asstB, err := mgr.CreateAssistantNode(userB.ID, "I am ready to investigate under George's lineage.", "", nil, false)
	if err != nil {
		t.Fatalf("failed to create asstB: %v", err)
	}
	asstB.Metadata["session_id"] = parentSessionID
	_ = sqlStore.SaveNode(asstB)
	if err := sqlStore.SaveSessionHead(parentSessionID, asstB.ID); err != nil {
		t.Fatalf("failed to save session head: %v", err)
	}

	// 4. Mock provider for subagent turn
	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			return "Subagent task completed under parent lineage.", "Finished", nil, nil
		},
	}

	orchestrator := NewSubagentOrchestrator(mgr, mockProvider, cfg)

	// 5. Spawn subagent from parent session
	res, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: parentSessionID,
		Task:            "Perform focused child audit",
		SessionLabel:    "child-lineage-audit",
		ToolPreset:      "read_only",
		IsolateWorktree: false,
		MaxSteps:        3,
	})
	if err != nil {
		t.Fatalf("SpawnSubagent failed: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected status completed, got %s (res: %+v)", res.Status, res)
	}

	// 6. Find subagent user node and assert it attached to Root B, NOT Root A
	var subagentUserNode *graph.Node
	for _, n := range mgr.Graph.GetAllNodes() {
		if n.Role == domain.RoleUser && n.Metadata != nil && n.Metadata["subagent"] == "true" {
			subagentUserNode = n
			break
		}
	}

	if subagentUserNode == nil {
		t.Fatalf("expected to find subagent user node in graph")
	}

	if subagentUserNode.ParentID != rootB.ID {
		t.Errorf("subagent attached to wrong parent ID: got %q, want %q (Root B). Did it attach to Root A (%q)?",
			subagentUserNode.ParentID, rootB.ID, rootA.ID)
	}

	if subagentUserNode.ParentID == rootA.ID {
		t.Errorf("FAIL: subagent incorrectly attached to oldest root (Root A) instead of parent branch root (Root B)!")
	}

	// 7. Verify LLM context for the subagent's user turn begins with Root B persona
	contextMsgs, err := mgr.BuildLLMContext(subagentUserNode.ID, false)
	if err != nil {
		t.Fatalf("BuildLLMContext failed: %v", err)
	}
	if len(contextMsgs) == 0 || contextMsgs[0].Role != domain.RoleSystem || !strings.Contains(contextMsgs[0].Content, "George the Archivist") {
		t.Errorf("expected LLM context to contain Root B persona ('George the Archivist'), got: %+v", contextMsgs)
	}
}



