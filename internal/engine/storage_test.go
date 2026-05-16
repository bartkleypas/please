package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestJSONLStorage(t *testing.T) {
	tmpFile := "test_graph.jsonl"
	defer os.Remove(tmpFile) // Clean up after test

	storage := NewJSONLStorage(tmpFile, "")
	now := time.Now()

	// 1. Create some nodes
	nodes := []*Node{
		{ID: "1_root", ParentID: "", Role: RoleSystem, Content: "System", Timestamp: now},
		{ID: "2", ParentID: "1_root", Role: RoleUser, Content: "Hello", Timestamp: now},
		{ID: "3", ParentID: "2", Role: RoleAssistant, Content: "Hi!", Timestamp: now},
	}

	// 2. Save nodes
	for _, n := range nodes {
		if err := storage.SaveNode(n); err != nil {
			t.Fatalf("SaveNode failed: %v", err)
		}
	}

	// 3. Load graph and verify
	graph, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(graph.Nodes))
	}

	// 4. Verify path integrity after loading
	path, err := graph.GetPath("3")
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
	nodes := []*Node{
		{ID: "1_root", Role: RoleSystem, Content: "Root", Timestamp: now, Metadata: map[string]string{"key": "val"}},
		{ID: "2", ParentID: "1_root", Role: RoleUser, Content: "Hello", Timestamp: now, Internal: true},
		{ID: "3", ParentID: "2", Role: RoleAssistant, Content: "World", Thought: "Thinking...", Timestamp: now, ToolCallID: "call_1"},
	}

	for _, n := range nodes {
		if err := storage.SaveNode(n); err != nil {
			t.Fatalf("SaveNode failed: %v", err)
		}
	}

	graph, lastID, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(graph.Nodes))
	}
	if lastID != "3" {
		t.Errorf("Expected lastID '3', got %s", lastID)
	}

	// Verify details
	root := graph.Nodes["1_root"]
	if root.Metadata["key"] != "val" {
		t.Errorf("Metadata not persisted")
	}

	n2 := graph.Nodes["3"]
	if n2.Thought != "Thinking..." {
		t.Errorf("Thought not persisted")
	}
	if n2.ToolCallID != "call_1" {
		t.Errorf("ToolCallID not persisted")
	}
	if !graph.Nodes["2"].Internal {
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

	node := &Node{
		ID:        "enc_1",
		Role:      RoleUser,
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

	// Verify LoadGraph decrypts properly
	graph, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}

	loadedNode, ok := graph.Nodes["enc_1"]
	if !ok {
		t.Fatalf("Node not loaded")
	}

	if loadedNode.Content != node.Content {
		t.Errorf("Expected decrypted content %q, got %q", node.Content, loadedNode.Content)
	}
	if loadedNode.Thought != node.Thought {
		t.Errorf("Expected decrypted thought %q, got %q", node.Thought, loadedNode.Thought)
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
				node := &Node{
					ID:        fmt.Sprintf("node-%d-%d", id, j),
					Role:      RoleUser,
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

	graph, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph after stress failed: %v", err)
	}

	expected := numGoroutines * nodesPerGoroutine
	if len(graph.Nodes) != expected {
		t.Errorf("Expected %d nodes after stress test, got %d", expected, len(graph.Nodes))
	}
}
