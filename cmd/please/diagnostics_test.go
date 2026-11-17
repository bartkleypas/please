package main

import (
	"fmt"
	"os"
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

func TestSetupDiagnosticsManager_AppliesConfigAndShaper(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "diag_vault.db")
	strg, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}
	root := &graph.Node{
		ID:        "root-1",
		Role:      domain.RoleSystem,
		Content:   "System",
		Timestamp: time.Now(),
	}
	if err := strg.SaveNode(root); err != nil {
		t.Fatalf("failed to save root node: %v", err)
	}

	cfgPath := filepath.Join(tempDir, "config.json")
	cfgJSON := fmt.Sprintf(`{
  "version": 2,
  "server": {
    "workspace_dir": %q,
    "vault_path": %q,
    "context_shaper": "window",
    "options": {
      "num_ctx": 65536
    }
  }
}`, tempDir, dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	mgr, _, err := setupDiagnosticsManager("", cfgPath)
	if err != nil {
		t.Fatalf("setupDiagnosticsManager failed: %v", err)
	}

	if mgr.NumCtx != 65536 {
		t.Errorf("expected NumCtx 65536 from config, got %d", mgr.NumCtx)
	}
	if mgr.ContextShaperType != "window" {
		t.Errorf("expected ContextShaperType 'window', got %q", mgr.ContextShaperType)
	}

	histNode := &graph.Node{
		ID:      "hist-user",
		Role:    domain.RoleUser,
		Content: "old question",
	}

	// Verify EvaluateNodeFidelity diverges appropriately by active ContextShaper under capacity pressure
	curves := []struct {
		name      string
		wantScore float64
		wantTier  engine.FidelityTier
	}{
		{name: "window", wantScore: 0.1, wantTier: engine.FidelityLow},
		{name: "sigmoid"},
		{name: "exponential"},
	}

	for _, c := range curves {
		mgr.Shaper = nil
		mgr.ContextShaperType = c.name
		score, tier := mgr.EvaluateNodeFidelity(histNode, 2, 12, 0.80, 15)
		if c.name == "window" {
			if score != c.wantScore || tier != c.wantTier {
				t.Errorf("window EvaluateNodeFidelity = (%v, %v), want (%v, %v)", score, tier, c.wantScore, c.wantTier)
			}
		} else if c.name == "sigmoid" {
			if tier != engine.FidelityLow || score <= 0 || score >= 0.15 {
				t.Errorf("sigmoid EvaluateNodeFidelity at d=12 = (%v, %v), expected low tier with (0, 0.15)", score, tier)
			}
		} else if c.name == "exponential" {
			if score <= 0 {
				t.Errorf("exponential EvaluateNodeFidelity expected positive score, got %v", score)
			}
			_ = tier.String()
		}
	}
}
