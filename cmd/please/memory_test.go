package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/storage"
)

func TestMemoryCLI_Commands(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_vault.db")

	sqliteStore, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	// Seed some test memories
	err = sqliteStore.SaveMemory(&storage.Memory{
		Key:        "arch_rule",
		Content:    "Store all graph nodes in WAL mode",
		Category:   storage.CategoryArchitecture,
		Scope:      storage.ScopeWorkspace,
		Confidence: 1.0,
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	err = sqliteStore.SaveMemory(&storage.Memory{
		Key:        "scratch_temp",
		Content:    "Investigating memory pressure",
		Category:   storage.CategoryScratchpad,
		Scope:      storage.ScopeWorkspace,
		Confidence: 0.8,
	})
	err = sqliteStore.SaveMemory(&storage.Memory{
		Key:        "workflow:test-caching-bypass",
		Content:    "Bypass go test cache with count=1",
		Category:   storage.CategoryWorkflow,
		Scope:      storage.ScopeWorkspace,
		Confidence: 1.0,
	})
	if err != nil {
		t.Fatalf("failed to save long key memory: %v", err)
	}

	// 1. Test memory list (human-readable)
	output := captureStdout(t, func() {
		runMemory([]string{"list", "-v", dbPath})
	})
	if !strings.Contains(output, "arch_rule") || !strings.Contains(output, "scratch_temp") {
		t.Errorf("expected memory list to display seeded keys, got:\n%s", output)
	}
	// Verify long key is NOT truncated to workflow:test-c..
	if !strings.Contains(output, "workflow:test-caching-bypass") {
		t.Errorf("expected full untruncated long key in list, got:\n%s", output)
	}

	// 2. Test memory list (JSON)
	outputJSON := captureStdout(t, func() {
		runMemory([]string{"list", "-v", dbPath, "--json"})
	})
	if !strings.Contains(outputJSON, `"key": "arch_rule"`) {
		t.Errorf("expected JSON memory list, got:\n%s", outputJSON)
	}

	// 3. Test memory inspect (exact match)
	outputInspect := captureStdout(t, func() {
		runMemory([]string{"inspect", "arch_rule", "-v", dbPath})
	})
	if !strings.Contains(outputInspect, "Store all graph nodes in WAL mode") {
		t.Errorf("expected memory inspect to show content, got:\n%s", outputInspect)
	}

	// 3b. Test memory inspect (fuzzy prefix match)
	outputInspectFuzzy := captureStdout(t, func() {
		runMemory([]string{"inspect", "caching-bypass", "-v", dbPath})
	})
	if !strings.Contains(outputInspectFuzzy, "Bypass go test cache with count=1") {
		t.Errorf("expected fuzzy inspect to find memory, got:\n%s", outputInspectFuzzy)
	}

	// 4. Test memory diagnose
	outputDiag := captureStdout(t, func() {
		runMemory([]string{"diagnose", "-v", dbPath})
	})
	if !strings.Contains(outputDiag, "Total Memories:   3") {
		t.Errorf("expected diagnose to show 3 memories, got:\n%s", outputDiag)
	}

	// 5. Test memory prune (dry run)
	outputPruneDry := captureStdout(t, func() {
		runMemory([]string{"prune", "-v", dbPath, "--category=scratchpad", "--dry-run"})
	})
	if !strings.Contains(outputPruneDry, "Dry Run") || !strings.Contains(outputPruneDry, "scratch_temp") {
		t.Errorf("expected prune dry run output, got:\n%s", outputPruneDry)
	}

	// 6. Test memory prune (actual deletion)
	outputPrune := captureStdout(t, func() {
		runMemory([]string{"prune", "-v", dbPath, "--category=scratchpad"})
	})
	if !strings.Contains(outputPrune, "Pruned 1/1 memories") {
		t.Errorf("expected pruned output, got:\n%s", outputPrune)
	}

	// Verify scratchpad memory was deleted but architecture and workflow memories remain
	mems, err := sqliteStore.QueryMemories(storage.MemoryFilter{})
	if err != nil {
		t.Fatalf("failed to query memories: %v", err)
	}
	if len(mems) != 2 {
		t.Fatalf("expected 2 remaining memories, got %d", len(mems))
	}
}

func TestMemoryCLI_JSONLVaultRejection(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "vault.jsonl")
	_ = os.WriteFile(jsonlPath, []byte("{}"), 0644)

	_, _, err := setupMemoryStore(jsonlPath, "")
	if err == nil {
		t.Fatalf("expected setupMemoryStore to reject jsonl vault")
	}
	if !strings.Contains(err.Error(), "requires an SQLite vault") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = oldStdout
	return <-outC
}
