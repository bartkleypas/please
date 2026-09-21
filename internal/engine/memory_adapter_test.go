package engine

import (
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
)

func TestNewMemoryToolsAdapter_Nil(t *testing.T) {
	adapter := NewMemoryToolsAdapter(nil)
	if adapter != nil {
		t.Errorf("expected nil adapter when store is nil, got %v", adapter)
	}
}

func TestMemoryToolsAdapter_RoundTrip(t *testing.T) {
	memStore := &MockMemoryStorage{
		memories: make(map[string]*storage.Memory),
	}
	adapter := NewMemoryToolsAdapter(memStore)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}

	// 1. Test SaveMemory with nil
	if err := adapter.SaveMemory(nil); err != nil {
		t.Fatalf("expected nil error on nil memory item, got %v", err)
	}

	// 2. Test SaveMemory with real item
	now := time.Now().Truncate(time.Second)
	item := &tools.MemoryItem{
		ID:             "mem-123",
		Key:            "arch_convention",
		Content:        "Store graph in WAL mode",
		Category:       "architecture",
		Scope:          "workspace",
		Tags:           []string{"sqlite", "wal"},
		Confidence:     1.0,
		SessionID:      "session-main",
		SourceNodeID:   "node-genesis",
		Metadata:       map[string]any{"author": "archivist"},
		AccessCount:    5,
		LastAccessedAt: &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := adapter.SaveMemory(item); err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	// Verify underlying store received correctly typed storage.Memory
	rawMem, err := memStore.GetMemory(storage.ScopeWorkspace, "session-main", "arch_convention")
	if err != nil || rawMem == nil {
		t.Fatalf("expected memory in underlying store: %v", err)
	}
	if rawMem.Category != storage.CategoryArchitecture {
		t.Errorf("expected category %q, got %q", storage.CategoryArchitecture, rawMem.Category)
	}
	if rawMem.Scope != storage.ScopeWorkspace {
		t.Errorf("expected scope %q, got %q", storage.ScopeWorkspace, rawMem.Scope)
	}

	// 3. Test GetMemory through adapter
	retrieved, err := adapter.GetMemory("workspace", "session-main", "arch_convention")
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected retrieved item to be non-nil")
	}
	if retrieved.Key != "arch_convention" || retrieved.Content != "Store graph in WAL mode" {
		t.Errorf("unexpected retrieved item: %+v", retrieved)
	}
	if retrieved.Category != "architecture" || retrieved.Scope != "workspace" {
		t.Errorf("unexpected category/scope: %s/%s", retrieved.Category, retrieved.Scope)
	}

	// 4. Test GetMemory not found
	missing, err := adapter.GetMemory("workspace", "session-main", "non_existent")
	if err != nil {
		t.Fatalf("expected nil error on missing key, got %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing key, got %+v", missing)
	}

	// 5. Test QueryMemories
	results, err := adapter.QueryMemories(tools.MemoryFilter{
		Scope:    "workspace",
		Category: "architecture",
	})
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}
	if len(results) != 1 || results[0].Key != "arch_convention" {
		t.Fatalf("expected 1 matching result, got %d", len(results))
	}

	// 6. Test DeleteMemory
	if err := adapter.DeleteMemory("workspace", "session-main", "arch_convention"); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}
	afterDelete, _ := adapter.GetMemory("workspace", "session-main", "arch_convention")
	if afterDelete != nil {
		t.Errorf("expected item to be deleted, but still found: %+v", afterDelete)
	}
}

func TestMemoryToolsAdapter_DiagnoseMemories(t *testing.T) {
	memStore := &MockMemoryStorage{
		memories: make(map[string]*storage.Memory),
	}
	now := time.Now()
	_ = memStore.SaveMemory(&storage.Memory{
		ID:          "m1",
		Key:         "key1",
		Content:     "content 1",
		Category:    storage.CategoryConstraint,
		Scope:       storage.ScopeWorkspace,
		AccessCount: 10,
		UpdatedAt:   now,
	})
	_ = memStore.SaveMemory(&storage.Memory{
		ID:          "m2",
		Key:         "key2",
		Content:     "content 2",
		Category:    storage.CategoryPreference,
		Scope:       storage.ScopeGlobal,
		AccessCount: 2,
		UpdatedAt:   now.Add(-24 * time.Hour),
	})

	adapter := NewMemoryToolsAdapter(memStore)
	diag, err := adapter.DiagnoseMemories("workspace", "")
	if err != nil {
		t.Fatalf("DiagnoseMemories failed: %v", err)
	}
	if diag == nil {
		t.Fatal("expected non-nil diagnostics")
	}
	if diag.TotalMemories != 2 {
		t.Errorf("expected 2 total memories, got %d", diag.TotalMemories)
	}
}
