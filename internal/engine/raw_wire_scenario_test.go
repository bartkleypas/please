package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/providers/wire"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
)

type mockGenerateRoundTripper func(req *http.Request) (*http.Response, error)

func (m mockGenerateRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m(req)
}

func TestScenario_RawWireProvider_MultiTurnSubagentToolLoop(t *testing.T) {
	repoDir, configDir := setupScenarioGitRepo(t)

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

	codec := wire.NewGemma4Codec()
	provider := providers.NewRawWireProvider("http://localhost:11434", "gemma4:e4b", codec, nil)

	var mu sync.Mutex
	parentCallIdx := 0
	childCallIdx := 0

	mockClient := &http.Client{
		Transport: mockGenerateRoundTripper(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			defer mu.Unlock()

			if req.URL.Path != "/api/generate" {
				t.Errorf("expected path /api/generate, got %s", req.URL.Path)
			}

			bodyBytes, _ := io.ReadAll(req.Body)
			var genReq struct {
				Model   string                 `json:"model"`
				Prompt  string                 `json:"prompt"`
				Raw     bool                   `json:"raw"`
				Stream  bool                   `json:"stream"`
				Options map[string]interface{} `json:"options"`
			}
			if err := json.Unmarshal(bodyBytes, &genReq); err != nil {
				t.Fatalf("failed to unmarshal generate request: %v", err)
			}

			// Validate raw wire protocol invariants
			if !genReq.Raw || !genReq.Stream {
				t.Errorf("expected raw: true and stream: true in request payload")
			}
			if !strings.HasSuffix(genReq.Prompt, "<|turn>model\n") && !strings.HasSuffix(genReq.Prompt, "<tool_response|>") {
				t.Errorf("expected prompt to end with <|turn>model\\n or <tool_response|>, got:\n%s", genReq.Prompt)
			}

			// Differentiate parent session from child subagent:
			// Per ADR 022 anti-recursion guard, spawn_subagent is never declared in the child's tool registry.
			isChild := !strings.Contains(genReq.Prompt, "declaration:spawn_subagent")

			var ndjson string

			if isChild {
				childCallIdx++
				switch childCallIdx {
				case 1:
					// Child Step 1: Read math.go
					ndjson = `{"response":"<|channel>thought\nInspecting math.go\n<channel|>","done":false}` + "\n" +
						`{"response":"<|tool_call>call:read_file{path:<|\"|>math.go<|\"|>}<tool_call|>","done":false}` + "\n" +
						`{"response":"","done":true}` + "\n"
				case 2:
					// Child Step 2: Edit math.go to fix bug
					ndjson = `{"response":"<|channel>thought\nReplacing buggy line\n<channel|>","done":false}` + "\n" +
						`{"response":"<|tool_call>call:edit_file{mode:<|\"|>replace_string<|\"|>,path:<|\"|>math.go<|\"|>,replace:<|\"|>return a + b<|\"|>,search:<|\"|>return a + b + 1 // Bug<|\"|>}<tool_call|>","done":false}` + "\n" +
						`{"response":"","done":true}` + "\n"
				case 3:
					// Child Step 3: Git commit
					ndjson = `{"response":"<|channel>thought\nCommitting fix\n<channel|>","done":false}` + "\n" +
						`{"response":"<|tool_call>call:execute_command{command:<|\"|>git add math.go && git commit -m \"fix: correct Add\"<|\"|>}<tool_call|>","done":false}` + "\n" +
						`{"response":"","done":true}` + "\n"
				default:
					// Child Step 4: Final synthesis
					ndjson = `{"response":"<|channel>thought\nFinished fixing bug.\n<channel|>","done":false}` + "\n" +
						`{"response":"Diagnosed off-by-one error in math.go. Replaced 'a + b + 1' with 'a + b'. Committed fix to isolated branch.","done":false}` + "\n" +
						`{"response":"","done":true}` + "\n"
				}
			} else {
				parentCallIdx++
				switch parentCallIdx {
				case 1:
					// Parent Step 1: Dispatch spawn_subagent
					ndjson = `{"response":"<|channel>thought\nSpawning child subagent to fix math bug\n<channel|>","done":false}` + "\n" +
						`{"response":"<|tool_call>call:spawn_subagent{isolate_worktree:true,max_steps:5,session_label:<|\"|>fix-math-bug<|\"|>,task:<|\"|>Investigate and fix TestAdd in math_test.go<|\"|>,tool_preset:<|\"|>full<|\"|>}<tool_call|>","done":false}` + "\n" +
						`{"response":"","done":true}` + "\n"
				default:
					// Parent Step 2: Synthesis
					ndjson = `{"response":"<|channel>thought\nMerge completed successfully.\n<channel|>","done":false}` + "\n" +
						`{"response":"The subagent completed its investigation and fixed the bug.","done":false}` + "\n" +
						`{"response":"","done":true}` + "\n"
				}
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(ndjson)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	provider.SetClient(mockClient)

	harness := NewSessionHarness(mgr, provider, cfg)

	ctx := context.Background()
	req := TurnRequest{
		SessionID:    "parent-session",
		Message:      "TestAdd is failing in math_test.go. Please dispatch a subagent to fix it.",
		Role:         string(domain.RoleUser),
		MaxToolDepth: 5,
	}

	resultNode, err := harness.ExecuteTurn(ctx, req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if resultNode == nil {
		t.Fatalf("expected non-nil resultNode")
	}

	t.Logf("parentCallIdx: %d, childCallIdx: %d", parentCallIdx, childCallIdx)
	if parentCallIdx < 2 {
		t.Fatalf("expected at least 2 parent turns, got %d", parentCallIdx)
	}
	if childCallIdx < 3 {
		t.Fatalf("expected at least 3 child turns, got %d", childCallIdx)
	}

	// Invariant 1: Primary repository file was NOT modified (worktree isolation)
	primaryContent, err := os.ReadFile(filepath.Join(repoDir, "math.go"))
	if err != nil {
		t.Fatalf("failed to read primary math.go: %v", err)
	}
	if !strings.Contains(string(primaryContent), "return a + b + 1") {
		t.Errorf("primary repo file was modified! Sandboxing failed: %s", string(primaryContent))
	}

	// Invariant 2: Perception shielding: parent has exactly 1 tool call (spawn_subagent)
	if len(resultNode.ToolCalls) != 1 {
		t.Fatalf("expected parent assistant node to have exactly 1 tool call, got %d", len(resultNode.ToolCalls))
	}
	if resultNode.ToolCalls[0].Function.Name != "spawn_subagent" {
		t.Errorf("expected spawn_subagent, got %s", resultNode.ToolCalls[0].Function.Name)
	}

	// Invariant 3: Observation contains structured SubagentResult
	if len(resultNode.Observations) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(resultNode.Observations))
	}
	var subRes tools.SubagentResult
	if err := json.Unmarshal([]byte(resultNode.Observations[0].Result), &subRes); err != nil {
		t.Fatalf("failed to unmarshal SubagentResult: %v\nResult: %s", err, resultNode.Observations[0].Result)
	}

	if subRes.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", subRes.Status)
	}
	if subRes.StepsUsed != 3 {
		t.Errorf("expected 3 steps used, got %d", subRes.StepsUsed)
	}
}
