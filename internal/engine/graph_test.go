package engine

import (
	"errors"
	"testing"
	"time"
)

func TestEngineGraph_ReExportsAndAliases(t *testing.T) {
	g := NewGraph()
	if g == nil {
		t.Fatal("expected NewGraph() to return non-nil Graph")
	}

	now := time.Now()
	root := &Node{
		ID:        "root",
		Role:      RoleSystem,
		Content:   "system prompt",
		Timestamp: now,
	}
	userNode := &Node{
		ID:        "user-1",
		ParentID:  "root",
		Role:      RoleUser,
		Content:   "hello",
		Timestamp: now.Add(time.Second),
	}

	g.AddNode(root)
	g.AddNode(userNode)

	// Verify GetPath on aliased type
	path, err := g.GetPath("user-1")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if len(path) != 2 || path[0].ID != "root" || path[1].ID != "user-1" {
		t.Errorf("unexpected path: %v", path)
	}

	// Verify ErrNodeNotFound re-export
	_, err = g.GetNode("non-existent")
	if err == nil || !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got: %v", err)
	}
}
