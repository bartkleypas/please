package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
)

func setupScenarioGitRepo(t *testing.T) (repoDir string, configDir string) {
	t.Helper()

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found, skipping living scenario test")
	}

	repoDir, _ = filepath.EvalSymlinks(t.TempDir())
	configDir, _ = filepath.EvalSymlinks(t.TempDir())

	runCmd := func(args ...string) {
		cmd := exec.Command(gitPath, append([]string{"-C", repoDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\nOutput: %s", strings.Join(args, " "), err, string(out))
		}
	}

	runCmd("init", "-b", "main")
	runCmd("config", "user.name", "Scenario Tester")
	runCmd("config", "user.email", "tester@please.dev")

	// Buggy implementation in primary repository
	mathSrc := `package main

func Add(a, b int) int {
	return a + b + 1 // Bug
}
`
	mathTest := `package main

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatalf("Add(2, 3) != 5")
	}
}
`
	if err := os.WriteFile(filepath.Join(repoDir, "math.go"), []byte(mathSrc), 0644); err != nil {
		t.Fatalf("failed to write math.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "math_test.go"), []byte(mathTest), 0644); err != nil {
		t.Fatalf("failed to write math_test.go: %v", err)
	}

	runCmd("add", ".")
	runCmd("commit", "-m", "Initial buggy commit")

	return repoDir, configDir
}

func TestScenario_DelegatedMultiAgentLivingScenario(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)

	// Configure environment so worktree uses configDir
	t.Setenv("PLEASE_CONFIG_DIR", configDir)

	dbPath := filepath.Join(configDir, "vault.db")
	sqlStore, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	g := graph.NewGraph()
	mgr := NewManager(g, sqlStore)
	mgr.WorkspaceDir = repoDir
	mgr.RegisterDefaultTools(repoDir)

	cfg := config.NewDefaultConfig()

	var mu sync.Mutex
	parentCallIdx := 0
	childCallIdx := 0

	mockProvider := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, toolSpecs []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			mu.Lock()
			defer mu.Unlock()

			// Differentiate parent session from child subagent
			isChild := false
			for _, m := range messages {
				if strings.Contains(m.Content, "fix-math-bug") || strings.Contains(m.Content, "Investigate and fix") {
					isChild = true
					break
				}
			}

			if isChild {
				childCallIdx++
				switch childCallIdx {
				case 1:
					// Child Step 1: Read buggy math.go
					return "", "Let me inspect math.go to find the bug.", []domain.ToolCall{
						{
							ID:   "child_call_1",
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
				case 2:
					// Child Step 2: Edit math.go to fix bug
					return "", "Found the bug: return a + b + 1. Fixing it now.", []domain.ToolCall{
						{
							ID:   "child_call_2",
							Type: "function",
							Function: struct {
								Name      string          `json:"name"`
								Arguments json.RawMessage `json:"arguments"`
							}{
								Name:      "edit_file",
								Arguments: json.RawMessage(`{"path": "math.go", "mode": "replace_string", "search": "return a + b + 1 // Bug", "replace": "return a + b"}`),
							},
						},
					}, nil
				case 3:
					// Child Step 3: Run git commit
					return "", "Commit the fix to the isolated branch.", []domain.ToolCall{
						{
							ID:   "child_call_3",
							Type: "function",
							Function: struct {
								Name      string          `json:"name"`
								Arguments json.RawMessage `json:"arguments"`
							}{
								Name:      "execute_command",
								Arguments: json.RawMessage(`{"command": "git add math.go && git commit -m \"fix: correct Add logic\""}`),
							},
						},
					}, nil
				default:
					// Child Step 4: Final synthesis yield
					return "Diagnosed off-by-one error in math.go. Replaced 'a + b + 1' with 'a + b'. Committed the fix to the isolated branch.", "Finished diagnosis.", nil, nil
				}
			}

			// Parent Turn
			parentCallIdx++
			if parentCallIdx == 1 {
				// Parent Turn Step 1: Dispatches spawn_subagent
				return "", "A test failure was reported. I will dispatch an isolated subagent to investigate and fix it.", []domain.ToolCall{
					{
						ID:   "call_spawn_1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name: "spawn_subagent",
							Arguments: json.RawMessage(`{
								"task": "Investigate and fix TestAdd in math_test.go",
								"session_label": "fix-math-bug",
								"tool_preset": "full",
								"isolate_worktree": true,
								"max_steps": 5
							}`),
						},
					},
				}, nil
			}

			// Parent Turn Step 2: Concludes to operator
			return "The subagent completed its investigation. An off-by-one bug in math.go was resolved and verified with unit tests on an isolated branch.", "Synthesizing answer", nil, nil
		},
	}

	harness := NewSessionHarness(mgr, mockProvider, cfg)

	// Execute parent turn
	req := TurnRequest{
		SessionID:    "main",
		Message:      "Please investigate and fix the failing test in math_test.go",
		Role:         string(domain.RoleUser),
		MaxToolDepth: 5,
	}

	parentAsstNode, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if parentAsstNode == nil {
		t.Fatal("expected non-nil parent assistant node")
	}

	// Invariant 1: Primary repository file was NOT modified
	primaryContent, err := os.ReadFile(filepath.Join(repoDir, "math.go"))
	if err != nil {
		t.Fatalf("failed to read primary math.go: %v", err)
	}
	if !strings.Contains(string(primaryContent), "return a + b + 1") {
		t.Errorf("primary repo file was modified! Sandboxing failed: %s", string(primaryContent))
	}

	// Invariant 2: Context shielding: Parent assistant node has only 1 tool call (spawn_subagent)
	if len(parentAsstNode.ToolCalls) != 1 {
		t.Fatalf("expected parent assistant node to have exactly 1 tool call, got %d", len(parentAsstNode.ToolCalls))
	}
	if parentAsstNode.ToolCalls[0].Function.Name != "spawn_subagent" {
		t.Errorf("expected parent tool call spawn_subagent, got %s", parentAsstNode.ToolCalls[0].Function.Name)
	}

	// Invariant 3: Observation contains structured SubagentResult
	if len(parentAsstNode.Observations) != 1 {
		t.Fatalf("expected parent to have exactly 1 observation, got %d", len(parentAsstNode.Observations))
	}
	obs := parentAsstNode.Observations[0]
	var subRes tools.SubagentResult
	if err := json.Unmarshal([]byte(obs.Result), &subRes); err != nil {
		t.Fatalf("failed to unmarshal subagent observation result: %v\nResult: %s", err, obs.Result)
	}

	if subRes.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", subRes.Status)
	}
	if subRes.StepsUsed != 3 {
		t.Errorf("expected 3 child steps used, got %d", subRes.StepsUsed)
	}
	if !strings.Contains(subRes.Synthesis, "Diagnosed off-by-one") {
		t.Errorf("expected synthesis in observation, got: %s", subRes.Synthesis)
	}
	if !strings.HasPrefix(subRes.Branch, "subsession/sub_") {
		t.Errorf("expected branch starting with subsession/sub_, got %s", subRes.Branch)
	}
	if len(subRes.FilesModified) != 1 || subRes.FilesModified[0] != "math.go" {
		t.Errorf("expected files_modified [math.go], got %v", subRes.FilesModified)
	}

	// Invariant 4: Check that the parent's LLM context is completely shielded
	ctxMessages, err := mgr.BuildLLMContext(parentAsstNode.ID, false)
	if err != nil {
		t.Fatalf("BuildLLMContext failed: %v", err)
	}
	for _, m := range ctxMessages {
		if strings.Contains(m.Content, "read_file") || strings.Contains(m.Content, "edit_file") {
			t.Errorf("child's internal tool steps leaked into parent context! Leaked: %s", m.Content)
		}
	}

	fmt.Printf("Living scenario verified in isolated worktree branch %s with commit %s\n", subRes.Branch, subRes.Commit)
}
