package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
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
	obs := []providers.ToolObservation{
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
		ToolCalls: []providers.ToolCall{
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
		Observations: []providers.ToolObservation{
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
