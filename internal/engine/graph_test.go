package engine

import (
	"strings"
	"testing"
	"time"
)

func TestGraph_GetPath(t *testing.T) {
	g := NewGraph()
	now := time.Now()

	// Setup a branching scenario:
	// Root -> NodeA -> NodeB
	//         \-> NodeC
	
	root := &Node{ID: "root", ParentID: "", Role: RoleSystem, Content: "System Prompt", Timestamp: now}
	nodeA := &Node{ID: "A", ParentID: "root", Role: RoleUser, Content: "Hello", Timestamp: now}
	nodeB := &Node{ID: "B", ParentID: "A", Role: RoleAssistant, Content: "Hi there!", Timestamp: now}
	nodeC := &Node{ID: "C", ParentID: "root", Role: RoleUser, Content: "Goodbye", Timestamp: now}

	g.AddNode(root)
	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)

	tests := []struct {
		name    string
		nodeID  string
		wantIDs []string
		wantErr bool
	}{
		{
			name:    "Path to root",
			nodeID:  "root",
			wantIDs: []string{"root"},
			wantErr: false,
		},
		{
			name:    "Path to leaf B",
			nodeID:  "B",
			wantIDs: []string{"root", "A", "B"},
			wantErr: false,
		},
		{
			name:    "Path to leaf C (branch)",
			nodeID:  "C",
			wantIDs: []string{"root", "C"},
			wantErr: false,
		},
		{
			name:    "Non-existent node",
			nodeID:  "Z",
			wantIDs: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := g.GetPath(tt.nodeID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(path) != len(tt.wantIDs) {
					t.Errorf("GetPath() got length %v, want %v", len(path), len(tt.wantIDs))
					return
				}
				for i, node := range path {
					if node.ID != tt.wantIDs[i] {
						t.Errorf("GetPath() node[%d] = %v, want %v", i, node.ID, tt.wantIDs[i])
					}
				}
			}
		})
	}
}

func TestGraph_GetPath_Cycle(t *testing.T) {
	g := NewGraph()
	now := time.Now()

	// Inject a cycle: A -> B -> A
	nodeA := &Node{ID: "A", ParentID: "B", Role: RoleUser, Content: "I am A", Timestamp: now}
	nodeB := &Node{ID: "B", ParentID: "A", Role: RoleAssistant, Content: "I am B", Timestamp: now}

	g.AddNode(nodeA)
	g.AddNode(nodeB)

	// GetPath should now return an error immediately instead of hanging
	path, err := g.GetPath("B")
	if err == nil {
		t.Errorf("GetPath() expected error for cyclic graph, got nil")
	} else if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("GetPath() expected cycle error, got: %v", err)
	}

	if path != nil {
		t.Errorf("GetPath() expected nil path for cyclic graph, got %v", path)
	}
}

func TestGraph_GetPath_Orphan(t *testing.T) {
	g := NewGraph()
	now := time.Now()

	// Inject an orphan: B points to A, but A is missing from the graph
	nodeB := &Node{ID: "B", ParentID: "A", Role: RoleAssistant, Content: "I am an orphan", Timestamp: now}
	g.AddNode(nodeB)

	path, err := g.GetPath("B")
	if err == nil {
		t.Errorf("GetPath() expected error for orphaned node, got nil")
	} else if !strings.Contains(err.Error(), "node not found") {
		t.Errorf("GetPath() expected 'node not found' error, got: %v", err)
	}

	if path != nil {
		t.Errorf("GetPath() expected nil path for orphan, got %v", path)
	}
}
