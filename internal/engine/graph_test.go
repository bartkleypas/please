package engine

import (
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
