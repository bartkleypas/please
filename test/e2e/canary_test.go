//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/storage"
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

	// 1. Directory inspection tool invocation (list_directory)
	turn1, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		ParentID: sysNode.ID,
		Message:  "Please use the list_directory tool to list the contents of the 'internal' directory and summarize what packages exist.",
		Role:     string(graph.RoleUser),
	}, nil)

	if err != nil {
		t.Fatalf("ExecuteTurn for tool invocation failed: %v", err)
	}
	if turn1 == nil || turn1.Content == "" {
		t.Fatal("expected non-empty assistant response for list_directory")
	}

	t.Logf("Canary directory listing completed: %s", strings.TrimSpace(turn1.Content))

	// 2. File read tool turn (read_file)
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

// TestE2E_Canary_MemoryTools verifies persistent agent memory storage, lineage attribution, and recall.
func TestE2E_Canary_MemoryTools(t *testing.T) {
	mgr, provider, cfg := setupE2E(t)
	ctx, cancel := e2eContext(t, 5*time.Minute)
	defer cancel()

	sysNode, err := mgr.CreateNode("", graph.RoleSystem, "You are a software engineer with access to cybernetic memory tools (memory_store, memory_recall). Use them when requested.", false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	harness := engine.NewSessionHarness(mgr, provider, cfg)

	// 1. Store an architectural constraint into the memory vault
	turn1, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		SessionID: "canary-session",
		ParentID:  sysNode.ID,
		Message:   "Please use the memory_store tool to save a workspace constraint with key 'constraint:hermetic-build', category 'constraint', and content 'Maintain hermetic zero-CGo build; avoid dynamic external linking.'",
		Role:      string(graph.RoleUser),
	}, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn for memory_store failed: %v", err)
	}
	if turn1 == nil {
		t.Fatal("expected non-nil assistant node from turn 1")
	}

	// Verify the memory was persisted in the SQLite store with correct lineage attribution
	memStore, ok := mgr.Storage.(storage.MemoryStore)
	if !ok {
		t.Fatal("expected manager storage to implement storage.MemoryStore")
	}

	mem, err := memStore.GetMemory(storage.ScopeWorkspace, "canary-session", "constraint:hermetic-build")
	if err != nil || mem == nil {
		// Fallback check without session ID if stored with empty session
		mem, err = memStore.GetMemory(storage.ScopeWorkspace, "", "constraint:hermetic-build")
	}
	if err != nil || mem == nil {
		t.Fatalf("memory 'constraint:hermetic-build' was not found in storage: %v", err)
	}
	if mem.Category != storage.CategoryConstraint {
		t.Errorf("expected category 'constraint', got %q", mem.Category)
	}
	if mem.SourceNodeID == "" {
		t.Errorf("expected SourceNodeID to be set for lineage attribution")
	}
	t.Logf("Canary memory stored successfully (ID: %s, Lineage: %s, Session: %s)", mem.ID, mem.SourceNodeID, mem.SessionID)

	// 2. Recall the stored memory
	turn2, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		SessionID: "canary-session",
		ParentID:  turn1.ID,
		Message:   "Please use the memory_recall tool to search for memories about 'hermetic' and report what you find.",
		Role:      string(graph.RoleUser),
	}, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn for memory_recall failed: %v", err)
	}
	if turn2 == nil || turn2.Content == "" {
		t.Fatal("expected non-empty assistant response for memory_recall")
	}

	t.Logf("Canary memory recall completed: %s", strings.TrimSpace(turn2.Content))
}
