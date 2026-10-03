package engine

import (
	"context"
	"encoding/json"
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
