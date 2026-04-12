package engine

import (
	"os"
	"testing"
	"time"
)

func TestJSONLStorage(t *testing.T) {
	tmpFile := "test_graph.jsonl"
	defer os.Remove(tmpFile) // Clean up after test

	storage := NewJSONLStorage(tmpFile)
	now := time.Now()

	// 1. Create some nodes
	nodes := []*Node{
		{ID: "root", ParentID: "", Role: RoleSystem, Content: "System", Timestamp: now},
		{ID: "1", ParentID: "root", Role: RoleUser, Content: "Hello", Timestamp: now},
		{ID: "2", ParentID: "1", Role: RoleAssistant, Content: "Hi!", Timestamp: now},
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
	path, err := graph.GetPath("2")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}

	if len(path) != 3 || path[0].ID != "root" || path[2].ID != "2" {
		t.Errorf("Path integrity lost after reload. Path: %v", path)
	}
}
