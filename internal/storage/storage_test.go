package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
)

func TestJSONLStorage(t *testing.T) {
	tmpFile := "test_graph.jsonl"
	defer os.Remove(tmpFile) // Clean up after test

	storage := NewJSONLStorage(tmpFile, "")
	now := time.Now()

	// 1. Create some nodes
	nodes := []*graph.Node{
		{ID: "1_root", ParentID: "", Role: graph.RoleSystem, Content: "System", Timestamp: now},
		{ID: "2", ParentID: "1_root", Role: graph.RoleUser, Content: "Hello", Timestamp: now},
		{ID: "3", ParentID: "2", Role: graph.RoleAssistant, Content: "Hi!", Timestamp: now},
	}

	// 2. Save nodes
	for _, n := range nodes {
		if err := storage.SaveNode(n); err != nil {
			t.Fatalf("SaveNode failed: %v", err)
		}
	}

	// 3. Load graph and verify
	g, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}

	if len(g.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(g.Nodes))
	}

	// 4. Verify path integrity after loading
	path, err := g.GetPath("3")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}

	if len(path) != 3 || path[0].ID != "1_root" || path[2].ID != "3" {
		t.Errorf("Path integrity lost after reload. Path: %v", path)
	}
}

func TestSQLiteStorage(t *testing.T) {
	tmpDB := "test_vault.db"
	defer os.Remove(tmpDB)
	defer os.Remove(tmpDB + "-shm")
	defer os.Remove(tmpDB + "-wal")

	storage, err := NewSQLiteStorage(tmpDB, "")
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}

	now := time.Now().Truncate(time.Second) // SQLite precision

	// 1. Test Saving and Loading
	nodes := []*graph.Node{
		{ID: "1_root", Role: graph.RoleSystem, Content: "Root", Timestamp: now, Metadata: map[string]string{"key": "val"}},
		{ID: "2", ParentID: "1_root", Role: graph.RoleUser, Content: "Hello", Timestamp: now, Internal: true},
		{ID: "3", ParentID: "2", Role: graph.RoleAssistant, Content: "World", Thought: "Thinking...", Timestamp: now, ToolCallID: "call_1"},
	}

	for _, n := range nodes {
		if err := storage.SaveNode(n); err != nil {
			t.Fatalf("SaveNode failed: %v", err)
		}
	}

	g, lastID, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}

	if len(g.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(g.Nodes))
	}
	if lastID != "3" {
		t.Errorf("Expected lastID '3', got %s", lastID)
	}

	// Verify details
	root := g.Nodes["1_root"]
	if root.Metadata["key"] != "val" {
		t.Errorf("Metadata not persisted")
	}

	n2 := g.Nodes["3"]
	if n2.Thought != "Thinking..." {
		t.Errorf("Thought not persisted")
	}
	if n2.ToolCallID != "call_1" {
		t.Errorf("ToolCallID not persisted")
	}
	if !g.Nodes["2"].Internal {
		t.Errorf("Internal flag not persisted")
	}

	// 2. Test Updates
	if err := storage.UpdateNodeParentID("3", "1_root"); err != nil {
		t.Fatalf("UpdateNodeParentID failed: %v", err)
	}

	n2.Deleted = true
	if err := storage.UpdateNodeMetadata(n2); err != nil {
		t.Fatalf("UpdateNodeMetadata failed: %v", err)
	}

	graph2, _, _ := storage.LoadGraph()
	if node2, ok := graph2.Nodes["3"]; ok {
		if node2.ParentID != "1_root" {
			t.Errorf("ParentID update failed, got %s", node2.ParentID)
		}
		t.Errorf("Deleted node should not be loaded into graph")
	}

	// 3. Test Garbage Collection
	affected, err := storage.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect failed: %v", err)
	}
	if affected != 1 {
		t.Errorf("Expected 1 row affected, got %d", affected)
	}

	// Double check deletion via raw SQL
	var count int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = '3'").Scan(&count)
	if err != nil {
		t.Fatalf("Raw query failed: %v", err)
	}
	if count != 0 {
		t.Error("GarbageCollect failed to permanently delete node")
	}
}

func TestSQLiteStorage_Encryption(t *testing.T) {
	tmpDB := "test_vault_enc.db"
	defer os.Remove(tmpDB)
	defer os.Remove(tmpDB + "-shm")
	defer os.Remove(tmpDB + "-wal")

	key := "my-secret-test-key-12345"
	storage, err := NewSQLiteStorage(tmpDB, key)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	node := &graph.Node{
		ID:        "enc_1",
		Role:      graph.RoleUser,
		Content:   "This is a highly secret message.",
		Thought:   "Top secret thought.",
		Timestamp: now,
	}

	if err := storage.SaveNode(node); err != nil {
		t.Fatalf("SaveNode failed: %v", err)
	}

	// Read directly from DB to verify it's encrypted
	var rawContent, rawThought string
	err = storage.db.QueryRow("SELECT content, thought FROM nodes WHERE id = 'enc_1'").Scan(&rawContent, &rawThought)
	if err != nil {
		t.Fatalf("Raw query failed: %v", err)
	}

	if rawContent == node.Content {
		t.Errorf("Content was not encrypted in DB")
	}
	if !strings.HasPrefix(rawContent, "enc:v1:") {
		t.Errorf("Content does not have encryption prefix, got: %s", rawContent)
	}

	if rawThought == node.Thought {
		t.Errorf("Thought was not encrypted in DB")
	}
	if !strings.HasPrefix(rawThought, "enc:v1:") {
		t.Errorf("Thought does not have encryption prefix, got: %s", rawThought)
	}

	// 2. Verify UpdateNodeObservations encrypts observations
	obs := []domain.ToolObservation{
		{ToolCallID: "call_abc", Result: "Confidential tool result."},
	}
	if err := storage.UpdateNodeObservations("enc_1", obs); err != nil {
		t.Fatalf("UpdateNodeObservations failed: %v", err)
	}

	// Read directly from DB to verify observations are encrypted
	var rawObs string
	err = storage.db.QueryRow("SELECT observations FROM nodes WHERE id = 'enc_1'").Scan(&rawObs)
	if err != nil {
		t.Fatalf("Raw query for observations failed: %v", err)
	}

	if rawObs == "[]" || rawObs == "" {
		t.Errorf("Observations were not updated in DB")
	}
	if !strings.HasPrefix(rawObs, "enc:v1:") {
		t.Errorf("Observations do not have encryption prefix, got: %s", rawObs)
	}

	// 3. Verify LoadGraph decrypts properly
	g, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}

	loadedNode, ok := g.Nodes["enc_1"]
	if !ok {
		t.Fatalf("Node not loaded")
	}

	if loadedNode.Content != node.Content {
		t.Errorf("Expected decrypted content %q, got %q", node.Content, loadedNode.Content)
	}
	if loadedNode.Thought != node.Thought {
		t.Errorf("Expected decrypted thought %q, got %q", node.Thought, loadedNode.Thought)
	}
	if len(loadedNode.Observations) != 1 || loadedNode.Observations[0].Result != "Confidential tool result." {
		t.Errorf("Expected decrypted observations, got: %v", loadedNode.Observations)
	}
}

func TestSQLiteStorage_Concurrency(t *testing.T) {
	tmpDB := "stress_vault.db"
	defer os.Remove(tmpDB)
	defer os.Remove(tmpDB + "-shm")
	defer os.Remove(tmpDB + "-wal")

	storage, err := NewSQLiteStorage(tmpDB, "")
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}

	const numGoroutines = 10
	const nodesPerGoroutine = 50
	done := make(chan bool)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < nodesPerGoroutine; j++ {
				node := &graph.Node{
					ID:        fmt.Sprintf("node-%d-%d", id, j),
					Role:      graph.RoleUser,
					Content:   "stress",
					Timestamp: time.Now(),
				}
				if err := storage.SaveNode(node); err != nil {
					t.Errorf("Concurrent SaveNode failed: %v", err)
				}
			}
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	g, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph after stress failed: %v", err)
	}

	expected := numGoroutines * nodesPerGoroutine
	if len(g.Nodes) != expected {
		t.Errorf("Expected %d nodes after stress test, got %d", expected, len(g.Nodes))
	}
}

func TestSQLiteStorage_Sessions(t *testing.T) {
	tmpDB := "test_sessions.db"
	defer os.Remove(tmpDB)
	defer os.Remove(tmpDB + "-shm")
	defer os.Remove(tmpDB + "-wal")

	storage, err := NewSQLiteStorage(tmpDB, "")
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}

	// 1. Initial lookup on empty DB returns empty string and nil error
	head, err := storage.GetSessionHead("main")
	if err != nil {
		t.Fatalf("GetSessionHead failed: %v", err)
	}
	if head != "" {
		t.Errorf("expected empty head, got %s", head)
	}

	// 2. Save session head for main
	if err := storage.SaveSessionHead("main", "node-101"); err != nil {
		t.Fatalf("SaveSessionHead failed: %v", err)
	}

	head, err = storage.GetSessionHead("main")
	if err != nil {
		t.Fatalf("GetSessionHead failed: %v", err)
	}
	if head != "node-101" {
		t.Errorf("expected node-101, got %s", head)
	}

	// 3. Save session head for another session
	if err := storage.SaveSessionHead("experiment", "node-202"); err != nil {
		t.Fatalf("SaveSessionHead failed: %v", err)
	}

	sessions, err := storage.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions["main"] != "node-101" || sessions["experiment"] != "node-202" {
		t.Errorf("sessions mismatch: %v", sessions)
	}

	// 4. Update existing session head
	if err := storage.SaveSessionHead("main", "node-102"); err != nil {
		t.Fatalf("SaveSessionHead update failed: %v", err)
	}
	head, err = storage.GetSessionHead("main")
	if err != nil || head != "node-102" {
		t.Errorf("expected updated head node-102, got %s (err: %v)", head, err)
	}
}

func TestJSONLStorage_Sessions(t *testing.T) {
	tmpFile := "test_sessions.jsonl"
	defer os.Remove(tmpFile)
	defer os.Remove(tmpFile + ".sessions.json")

	storage := NewJSONLStorage(tmpFile, "")

	// 1. Empty lookup
	head, err := storage.GetSessionHead("main")
	if err != nil || head != "" {
		t.Fatalf("expected empty head, got %q, err %v", head, err)
	}

	// 2. Save & List
	if err := storage.SaveSessionHead("felicia", "node-555"); err != nil {
		t.Fatalf("SaveSessionHead failed: %v", err)
	}
	head, err = storage.GetSessionHead("felicia")
	if err != nil || head != "node-555" {
		t.Fatalf("expected node-555, got %q, err %v", head, err)
	}

	sessions, err := storage.ListSessions()
	if err != nil || sessions["felicia"] != "node-555" {
		t.Fatalf("unexpected sessions list: %v", sessions)
	}
}

func TestSQLiteStorage_SaveNodePreservesExistingObservations(t *testing.T) {
	tmpDB := t.TempDir() + "/test_obs_preservation.db"
	storage, err := NewSQLiteStorage(tmpDB, "")
	if err != nil {
		t.Fatalf("failed to init SQLiteStorage: %v", err)
	}

	// 1. Save assistant node with tool calls and observations
	asstNode := &graph.Node{
		ID:        "asst-1",
		Role:      graph.RoleAssistant,
		Content:   "Reading log file...",
		Timestamp: time.Now(),
		ToolCalls: []domain.ToolCall{
			{
				ID: "call_read_log",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"log.md"}`),
				},
			},
		},
		Observations: []domain.ToolObservation{
			{
				ToolCallID: "call_read_log",
				Result:     "# Log Content\nYesterday we refactored the DAG.",
			},
		},
	}

	if err := storage.SaveNode(asstNode); err != nil {
		t.Fatalf("SaveNode failed: %v", err)
	}

	// Verify observations stored
	g, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}
	loaded, err := g.GetNode("asst-1")
	if err != nil || len(loaded.Observations) != 1 {
		t.Fatalf("expected 1 observation, got %d (err: %v)", len(loaded.Observations), err)
	}

	// 2. Simulate subsequent update where caller provides empty/nil Observations
	// (e.g. updating Content or Metadata without having observations loaded in-memory)
	updatedNode := &graph.Node{
		ID:           "asst-1",
		Role:         graph.RoleAssistant,
		Content:      "Based on the log, yesterday we refactored the DAG. 🦉☕",
		Timestamp:    asstNode.Timestamp,
		ToolCalls:    asstNode.ToolCalls,
		Observations: nil, // Nil observations!
		Metadata:     map[string]string{"signat": "🦉☕"},
	}

	if err := storage.SaveNode(updatedNode); err != nil {
		t.Fatalf("second SaveNode failed: %v", err)
	}

	// 3. Load graph and verify observations were PRESERVED and not wiped out!
	g2, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph 2 failed: %v", err)
	}
	loaded2, err := g2.GetNode("asst-1")
	if err != nil {
		t.Fatalf("node not found after second save: %v", err)
	}

	if len(loaded2.Observations) != 1 {
		t.Fatalf("CRITICAL: observations were wiped out by SaveNode! Expected 1, got %d", len(loaded2.Observations))
	}
	if loaded2.Observations[0].ToolCallID != "call_read_log" {
		t.Errorf("expected observation for call_read_log, got: %s", loaded2.Observations[0].ToolCallID)
	}
	if !strings.Contains(loaded2.Observations[0].Result, "Yesterday we refactored the DAG") {
		t.Errorf("observation content was corrupted: %q", loaded2.Observations[0].Result)
	}
	if loaded2.Content != updatedNode.Content {
		t.Errorf("content was not updated: %q", loaded2.Content)
	}
}

func TestSQLiteStorage_MemoriesCRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memories-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	// 1. Save new memory
	mem := &Memory{
		Key:      "arch:storage:wal",
		Content:  "SQLite WAL mode requires SetMaxOpenConns(1) to avoid BUSY errors.",
		Category: CategoryConstraint,
		Tags:     []string{"sqlite", "concurrency", "wal"},
		Scope:    ScopeWorkspace,
	}

	if err := storage.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}
	if mem.ID == "" {
		t.Errorf("expected generated UUID, got empty")
	}

	// 2. Get memory
	retrieved, err := storage.GetMemory(ScopeWorkspace, "", "arch:storage:wal")
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}
	if retrieved == nil {
		t.Fatalf("expected memory, got nil")
	}
	if retrieved.Content != mem.Content {
		t.Errorf("expected content %q, got %q", mem.Content, retrieved.Content)
	}
	if retrieved.Category != CategoryConstraint {
		t.Errorf("expected category %s, got %s", CategoryConstraint, retrieved.Category)
	}
	if len(retrieved.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(retrieved.Tags))
	}
	if retrieved.AccessCount != 0 {
		t.Errorf("expected access count 0 after passive get, got %d", retrieved.AccessCount)
	}

	// Active recall with Touch: true increments access telemetry
	recalled, err := storage.QueryMemories(MemoryFilter{Key: "arch:storage:wal", Touch: true})
	if err != nil || len(recalled) == 0 {
		t.Fatalf("QueryMemories with Touch failed: %v", err)
	}
	if recalled[0].AccessCount != 1 {
		t.Errorf("expected access count 1 after active recall, got %d", recalled[0].AccessCount)
	}

	// 3. Upsert update
	mem.Content = "Updated content for WAL."
	if err := storage.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory upsert failed: %v", err)
	}

	updated, err := storage.GetMemory(ScopeWorkspace, "", "arch:storage:wal")
	if err != nil {
		t.Fatalf("GetMemory after update failed: %v", err)
	}
	if updated.Content != "Updated content for WAL." {
		t.Errorf("expected updated content, got %q", updated.Content)
	}

	// 4. Delete memory
	if err := storage.DeleteMemory(ScopeWorkspace, "", "arch:storage:wal"); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}
	deleted, err := storage.GetMemory(ScopeWorkspace, "", "arch:storage:wal")
	if err != nil {
		t.Fatalf("GetMemory after delete returned error: %v", err)
	}
	if deleted != nil {
		t.Errorf("expected nil after delete, got %+v", deleted)
	}
}

func TestSQLiteStorage_MemoriesFTS(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memories-fts-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	_ = storage.SaveMemory(&Memory{
		Key:      "constraint:cgo",
		Content:  "Zero-CGo hermetic compilation is strictly enforced.",
		Category: CategoryConstraint,
		Tags:     []string{"cgo", "compiler"},
		Scope:    ScopeWorkspace,
	})

	_ = storage.SaveMemory(&Memory{
		Key:      "workflow:bell",
		Content:  "The terminal bell emits ASCII 0x07 upon turn conclusion.",
		Category: CategoryWorkflow,
		Tags:     []string{"telemetry", "bell"},
		Scope:    ScopeWorkspace,
	})

	// Query with keyword "hermetic"
	results, err := storage.QueryMemories(MemoryFilter{
		Query: "hermetic",
		Scope: ScopeWorkspace,
	})
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result matching 'hermetic', got %d", len(results))
	}
	if results[0].Key != "constraint:cgo" {
		t.Errorf("expected 'constraint:cgo', got %s", results[0].Key)
	}

	// Query with tag "bell"
	tagResults, err := storage.QueryMemories(MemoryFilter{
		Tags:  []string{"bell"},
		Scope: ScopeWorkspace,
	})
	if err != nil {
		t.Fatalf("QueryMemories with tags failed: %v", err)
	}
	if len(tagResults) != 1 || tagResults[0].Key != "workflow:bell" {
		t.Errorf("expected workflow:bell, got %+v", tagResults)
	}
}

func TestSQLiteStorage_MemoriesSessionIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memories-iso-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	// Workspace memory (visible to all)
	_ = storage.SaveMemory(&Memory{
		Key:      "repo:name",
		Content:  "Please TUI",
		Category: CategoryFact,
		Scope:    ScopeWorkspace,
	})

	// Session A scratchpad
	_ = storage.SaveMemory(&Memory{
		Key:       "scratchpad:notes",
		Content:   "Notes from Session A",
		Category:  CategoryScratchpad,
		Scope:     ScopeSession,
		SessionID: "session-a",
	})

	// Session B scratchpad with identical key!
	_ = storage.SaveMemory(&Memory{
		Key:       "scratchpad:notes",
		Content:   "Notes from Session B",
		Category:  CategoryScratchpad,
		Scope:     ScopeSession,
		SessionID: "session-b",
	})

	// 1. Session A query: sees repo:name and Session A notes, NOT Session B notes
	resA, err := storage.QueryMemories(MemoryFilter{
		SessionID: "session-a",
	})
	if err != nil {
		t.Fatalf("QueryMemories for session-a failed: %v", err)
	}
	if len(resA) != 2 {
		t.Fatalf("expected 2 memories for session-a, got %d", len(resA))
	}
	foundA := false
	for _, m := range resA {
		if m.SessionID == "session-b" {
			t.Errorf("CRITICAL: session-a saw session-b memory: %+v", m)
		}
		if m.Content == "Notes from Session A" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("expected to find Session A notes in resA")
	}

	// 2. Session B query: sees repo:name and Session B notes, NOT Session A notes
	resB, err := storage.QueryMemories(MemoryFilter{
		SessionID: "session-b",
	})
	if err != nil {
		t.Fatalf("QueryMemories for session-b failed: %v", err)
	}
	if len(resB) != 2 {
		t.Fatalf("expected 2 memories for session-b, got %d", len(resB))
	}
	foundB := false
	for _, m := range resB {
		if m.SessionID == "session-a" {
			t.Errorf("CRITICAL: session-b saw session-a memory: %+v", m)
		}
		if m.Content == "Notes from Session B" {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("expected to find Session B notes in resB")
	}
}

func TestSQLiteStorage_MemoriesEncryption(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memories-enc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	encKey := "topsecret-encryption-key-32-chars!"
	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, err := NewSQLiteStorage(dbPath, encKey)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	secretContent := "Secret vault invariant: API_TOKEN is confidential."
	_ = storage.SaveMemory(&Memory{
		Key:      "secret:note",
		Content:  secretContent,
		Category: CategoryFact,
		Scope:    ScopeWorkspace,
	})

	// Get with proper key
	mem, err := storage.GetMemory(ScopeWorkspace, "", "secret:note")
	if err != nil || mem == nil {
		t.Fatalf("failed to get memory with valid key: %v", err)
	}
	if mem.Content != secretContent {
		t.Errorf("expected %q, got %q", secretContent, mem.Content)
	}

	// Verify raw content in SQLite file is actually encrypted and ciphertext does not contain plain secret
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw sqlite: %v", err)
	}
	defer rawDB.Close()

	var rawContent string
	if err := rawDB.QueryRow("SELECT content FROM memories WHERE key = 'secret:note'").Scan(&rawContent); err != nil {
		t.Fatalf("failed to read raw content: %v", err)
	}
	if strings.Contains(rawContent, "Secret vault invariant") {
		t.Errorf("raw content in DB was unencrypted! got: %s", rawContent)
	}
}

func TestSQLiteStorage_MemoriesDiagnostics(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memories-diag-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	_ = storage.SaveMemory(&Memory{Key: "k1", Content: "c1", Category: CategoryArchitecture, Scope: ScopeWorkspace})
	_ = storage.SaveMemory(&Memory{Key: "k2", Content: "c2", Category: CategoryConstraint, Scope: ScopeWorkspace})
	_ = storage.SaveMemory(&Memory{Key: "k3", Content: "c3", Category: CategoryPreference, Scope: ScopeGlobal})

	diag, err := storage.DiagnoseMemories("all", "")
	if err != nil {
		t.Fatalf("DiagnoseMemories failed: %v", err)
	}
	if diag.TotalMemories != 3 {
		t.Errorf("expected 3 total memories, got %d", diag.TotalMemories)
	}
	if diag.ByScope[ScopeWorkspace] != 2 {
		t.Errorf("expected 2 workspace memories, got %d", diag.ByScope[ScopeWorkspace])
	}
	if diag.ByScope[ScopeGlobal] != 1 {
		t.Errorf("expected 1 global memory, got %d", diag.ByScope[ScopeGlobal])
	}
	if diag.ByCategory[CategoryArchitecture] != 1 {
		t.Errorf("expected 1 architecture memory, got %d", diag.ByCategory[CategoryArchitecture])
	}
	if diag.StorageBytes <= 0 {
		t.Errorf("expected positive storage bytes, got %d", diag.StorageBytes)
	}
}
