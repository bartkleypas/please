package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/storage"
)

func TestResolveDiagnosticNode(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_vault.db")

	strg, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	g := graph.NewGraph()
	mgr := engine.NewManager(g, strg)

	now := time.Now()
	root := &graph.Node{
		ID:        "root-0001",
		Role:      domain.RoleSystem,
		Content:   "You are an assistant.",
		Timestamp: now.Add(-10 * time.Minute),
	}
	user := &graph.Node{
		ID:        "user-0002",
		ParentID:  root.ID,
		Role:      domain.RoleUser,
		Content:   "Hello!",
		Timestamp: now.Add(-5 * time.Minute),
	}
	leaf := &graph.Node{
		ID:        "asst-0003-leaf-node-xyz",
		ParentID:  user.ID,
		Role:      domain.RoleAssistant,
		Content:   "Hi there!",
		Timestamp: now,
	}

	g.AddNode(root)
	g.AddNode(user)
	g.AddNode(leaf)

	// 1. Resolve empty query -> should return latest leaf
	resolved, err := resolveDiagnosticNode(mgr, "")
	if err != nil {
		t.Fatalf("unexpected error on empty query: %v", err)
	}
	if resolved.ID != leaf.ID {
		t.Errorf("expected latest leaf %q, got %q", leaf.ID, resolved.ID)
	}

	// 2. Resolve exact full ID
	resolved, err = resolveDiagnosticNode(mgr, "user-0002")
	if err != nil {
		t.Fatalf("unexpected error on exact ID: %v", err)
	}
	if resolved.ID != user.ID {
		t.Errorf("expected user-0002, got %q", resolved.ID)
	}

	// 3. Resolve short prefix
	resolved, err = resolveDiagnosticNode(mgr, "asst-0003")
	if err != nil {
		t.Fatalf("unexpected error on short prefix: %v", err)
	}
	if resolved.ID != leaf.ID {
		t.Errorf("expected leaf ID %q, got %q", leaf.ID, resolved.ID)
	}

	// 4. Resolve nonexistent ID
	_, err = resolveDiagnosticNode(mgr, "nonexistent-id")
	if err == nil {
		t.Errorf("expected error for nonexistent ID, got nil")
	}
}

func TestNormalizeFlagArgs(t *testing.T) {
	cases := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"node-123", "-v", "vault.db", "-c", "config.json"},
			expected: []string{"-v", "vault.db", "-c", "config.json", "node-123"},
		},
		{
			input:    []string{"-v", "vault.db", "node-123"},
			expected: []string{"-v", "vault.db", "node-123"},
		},
		{
			input:    []string{"node-123", "--json"},
			expected: []string{"--json", "node-123"},
		},
	}

	for _, c := range cases {
		out := normalizeFlagArgs(c.input)
		if len(out) != len(c.expected) {
			t.Fatalf("length mismatch: got %v, expected %v", out, c.expected)
		}
		for i := range out {
			if out[i] != c.expected[i] {
				t.Errorf("index %d mismatch: got %q, expected %q", i, out[i], c.expected[i])
			}
		}
	}
}
