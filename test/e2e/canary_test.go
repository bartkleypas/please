//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
)

// TestE2E_Canary_Narrator verifies basic model connectivity and prompt completion.
func TestE2E_Canary_Narrator(t *testing.T) {
	mgr, provider, cfg := setupE2E(t)
	ctx, cancel := e2eContext(t, 2*time.Minute)
	defer cancel()

	sysNode, err := mgr.CreateNode("", graph.RoleSystem, "You are a helpful and concise narrator.", false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	harness := engine.NewSessionHarness(mgr, provider, cfg)
	asstNode, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		ParentID: sysNode.ID,
		Message:  "Hello! Can you confirm you are online?",
		Role:     string(graph.RoleUser),
	}, nil)

	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if asstNode == nil || asstNode.Content == "" {
		t.Fatal("expected non-empty assistant response")
	}

	t.Logf("Canary Narrator confirmed online: %s", asstNode.Content)
}

// TestE2E_Canary_ToolExecution verifies structured tool execution and observation feedback.
func TestE2E_Canary_ToolExecution(t *testing.T) {
	mgr, provider, cfg := setupE2E(t)
	ctx, cancel := e2eContext(t, 5*time.Minute)
	defer cancel()

	sysNode, err := mgr.CreateNode("", graph.RoleSystem, "You are a helpful assistant with access to tools. Always use available tools when asked.", false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	harness := engine.NewSessionHarness(mgr, provider, cfg)

	// 1. Basic tool invocation (execute_command date)
	turn1, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		ParentID: sysNode.ID,
		Message:  "Please use the execute_command tool to run 'date' and report the result.",
		Role:     string(graph.RoleUser),
	}, nil)

	if err != nil {
		t.Fatalf("ExecuteTurn for tool invocation failed: %v", err)
	}
	if turn1 == nil {
		t.Fatal("expected non-nil assistant node")
	}

	t.Logf("Canary tool execution completed: %s", turn1.Content)

	// 2. File search / read tool turn
	turn2, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		ParentID: turn1.ID,
		Message:  "Please read the first 5 lines of README.md using the read_file tool and summarize them.",
		Role:     string(graph.RoleUser),
	}, nil)

	if err != nil {
		t.Fatalf("ExecuteTurn for file reading failed: %v", err)
	}
	if turn2 == nil || turn2.Content == "" {
		t.Fatal("expected non-empty assistant response for read_file")
	}

	t.Logf("Canary file read completed: %s", strings.TrimSpace(turn2.Content))
}
