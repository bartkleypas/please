package graph

import (
	"errors"
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

	root := &Node{ID: "1_root", ParentID: "", Role: RoleSystem, Content: "System Prompt", Timestamp: now}
	nodeA := &Node{ID: "2_A", ParentID: "1_root", Role: RoleUser, Content: "Hello", Timestamp: now.Add(time.Second)}
	nodeB := &Node{ID: "3_B", ParentID: "2_A", Role: RoleAssistant, Content: "Hi there!", Timestamp: now.Add(2 * time.Second)}
	nodeC := &Node{ID: "4_C", ParentID: "1_root", Role: RoleUser, Content: "Goodbye", Timestamp: now.Add(3 * time.Second)}

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
			nodeID:  "1_root",
			wantIDs: []string{"1_root"},
			wantErr: false,
		},
		{
			name:    "Path to leaf B",
			nodeID:  "3_B",
			wantIDs: []string{"1_root", "2_A", "3_B"},
			wantErr: false,
		},
		{
			name:    "Path to leaf C (branch)",
			nodeID:  "4_C",
			wantIDs: []string{"1_root", "4_C"},
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
	nodeA := &Node{ID: "2_A", ParentID: "3_B", Role: RoleUser, Content: "I am A", Timestamp: now}
	nodeB := &Node{ID: "3_B", ParentID: "2_A", Role: RoleAssistant, Content: "I am B", Timestamp: now}

	g.AddNode(nodeA)
	g.AddNode(nodeB)

	// GetPath should now return an error immediately instead of hanging
	path, err := g.GetPath("3_B")
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
	nodeB := &Node{ID: "3_B", ParentID: "2_A", Role: RoleAssistant, Content: "I am an orphan", Timestamp: now}
	g.AddNode(nodeB)

	path, err := g.GetPath("3_B")
	if err == nil {
		t.Errorf("GetPath() expected error for orphaned node, got nil")
	} else if !strings.Contains(err.Error(), "node not found") {
		t.Errorf("GetPath() expected 'node not found' error, got: %v", err)
	}

	if path != nil {
		t.Errorf("GetPath() expected nil path for orphan, got %v", path)
	}
}

func TestGraph_MutationsAndLookups(t *testing.T) {
	g := NewGraph()
	now := time.Now()

	root := &Node{ID: "node-root-12345", Role: RoleSystem, Content: "system", Timestamp: now}
	active := &Node{ID: "node-user-67890", ParentID: root.ID, Role: RoleUser, Content: "hello", Timestamp: now.Add(time.Second)}
	deleted := &Node{ID: "node-del-99999", ParentID: root.ID, Role: RoleUser, Content: "gone", Deleted: true, Timestamp: now.Add(2 * time.Second)}

	g.AddNode(root)
	g.AddNode(active)
	g.AddNode(deleted)

	// GetAllNodes (only active)
	allNodes := g.GetAllNodes()
	if len(allNodes) != 2 {
		t.Errorf("expected 2 active nodes, got %d", len(allNodes))
	}

	// GetNode
	got, err := g.GetNode(active.ID)
	if err != nil || got.ID != active.ID {
		t.Errorf("GetNode failed: %v", err)
	}

	// GetNode non-existent
	_, err = g.GetNode("non-existent")
	if err == nil || !strings.Contains(err.Error(), ErrNodeNotFound.Error()) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}

	// FindNodeByShortID prefix
	found, err := g.FindNodeByShortID("node-user")
	if err != nil || found.ID != active.ID {
		t.Errorf("FindNodeByShortID prefix failed: %v", err)
	}

	// FindNodeByShortID suffix
	found, err = g.FindNodeByShortID("67890")
	if err != nil || found.ID != active.ID {
		t.Errorf("FindNodeByShortID suffix failed: %v", err)
	}

	// FindNodeByShortID empty
	_, err = g.FindNodeByShortID("")
	if err == nil {
		t.Errorf("expected error on empty shortID")
	}

	// FindNodeByShortID not found
	_, err = g.FindNodeByShortID("zzzz")
	if err == nil {
		t.Errorf("expected error on unmatched shortID")
	}
}

func TestGraph_ChildrenAndRoots(t *testing.T) {
	g := NewGraph()
	now := time.Now()

	root1 := &Node{ID: "root-1", Role: RoleSystem, Content: "system 1", Timestamp: now}
	root2 := &Node{ID: "root-2", Role: RoleSystem, Content: "system 2", Timestamp: now.Add(time.Second)}
	child1 := &Node{ID: "child-1", ParentID: root1.ID, Role: RoleUser, Content: "c1", Timestamp: now.Add(2 * time.Second)}
	child2 := &Node{ID: "child-2", ParentID: root1.ID, Role: RoleUser, Content: "c2", Timestamp: now.Add(3 * time.Second)}

	g.AddNode(root2)
	g.AddNode(root1)
	g.AddNode(child2)
	g.AddNode(child1)

	// Roots should be sorted by timestamp: root-1 then root-2
	roots := g.GetRoots()
	if len(roots) != 2 || roots[0].ID != "root-1" || roots[1].ID != "root-2" {
		t.Errorf("roots not sorted correctly: %v", roots)
	}

	// SystemRoot should return root-1
	sysRoot, err := g.GetSystemRoot()
	if err != nil || sysRoot.ID != "root-1" {
		t.Errorf("GetSystemRoot failed: %v", err)
	}

	// Children should be sorted by timestamp: child-1 then child-2
	children := g.GetChildren(root1.ID)
	if len(children) != 2 || children[0].ID != "child-1" || children[1].ID != "child-2" {
		t.Errorf("children not sorted correctly: %v", children)
	}

	// Empty children
	noChildren := g.GetChildren("non-existent")
	if len(noChildren) != 0 {
		t.Errorf("expected 0 children for non-existent parent, got %d", len(noChildren))
	}
}

func TestGraph_GetSystemRoot_Errors(t *testing.T) {
	g := NewGraph()
	_, err := g.GetSystemRoot()
	if err == nil || !strings.Contains(err.Error(), "no root node found") {
		t.Errorf("expected no root node found error, got %v", err)
	}

	userRoot := &Node{ID: "user-root", Role: RoleUser, Content: "not system"}
	g.AddNode(userRoot)
	_, err = g.GetSystemRoot()
	if err == nil || !strings.Contains(err.Error(), "root node is not a system prompt") {
		t.Errorf("expected root node is not a system prompt error, got %v", err)
	}
}

func TestGraph_GetSystemRoot_MultipleRoots(t *testing.T) {
	g := NewGraph()
	userRoot := &Node{ID: "user-root", Role: RoleUser, Content: "user prompt"}
	g.AddNode(userRoot)
	sysRoot := &Node{ID: "sys-root", Role: RoleSystem, Content: "system prompt"}
	g.AddNode(sysRoot)

	found, err := g.GetSystemRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != "sys-root" {
		t.Errorf("expected sys-root, got %s", found.ID)
	}

	aliasFound, err := g.GetRootNode()
	if err != nil || aliasFound.ID != "sys-root" {
		t.Errorf("GetRootNode failed: %v", err)
	}
}

func TestGraph_GetRootOf(t *testing.T) {
	g := NewGraph()

	now := time.Now()
	rootA := &Node{ID: "root-a", Role: RoleSystem, Content: "System A", Timestamp: now}
	childA1 := &Node{ID: "child-a1", ParentID: "root-a", Role: RoleUser, Content: "User A1", Timestamp: now.Add(time.Second)}
	childA2 := &Node{ID: "child-a2", ParentID: "child-a1", Role: RoleAssistant, Content: "Asst A2", Timestamp: now.Add(2 * time.Second)}

	rootB := &Node{ID: "root-b", Role: RoleSystem, Content: "System B", Timestamp: now.Add(3 * time.Second)}
	childB1 := &Node{ID: "child-b1", ParentID: "root-b", Role: RoleUser, Content: "User B1", Timestamp: now.Add(4 * time.Second)}

	g.AddNode(rootA)
	g.AddNode(childA1)
	g.AddNode(childA2)
	g.AddNode(rootB)
	g.AddNode(childB1)

	// Leaf of lineage A resolves to root-a
	rA, err := g.GetRootOf("child-a2")
	if err != nil {
		t.Fatalf("GetRootOf child-a2 failed: %v", err)
	}
	if rA.ID != "root-a" {
		t.Errorf("expected root-a, got %s", rA.ID)
	}

	// Mid-node of lineage A resolves to root-a
	rA1, err := g.GetRootOf("child-a1")
	if err != nil || rA1.ID != "root-a" {
		t.Errorf("expected root-a for child-a1, got %v (err: %v)", rA1, err)
	}

	// Root itself resolves to itself
	rRootA, err := g.GetRootOf("root-a")
	if err != nil || rRootA.ID != "root-a" {
		t.Errorf("expected root-a for root-a, got %v (err: %v)", rRootA, err)
	}

	// Lineage B resolves to root-b
	rB, err := g.GetRootOf("child-b1")
	if err != nil {
		t.Fatalf("GetRootOf child-b1 failed: %v", err)
	}
	if rB.ID != "root-b" {
		t.Errorf("expected root-b, got %s", rB.ID)
	}

	// Empty node ID returns error
	if _, err := g.GetRootOf(""); err == nil {
		t.Errorf("expected error for empty nodeID")
	}

	// Non-existent node returns ErrNodeNotFound
	if _, err := g.GetRootOf("non-existent"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGraph_GetRootOf_CycleDetection(t *testing.T) {
	g := NewGraph()

	// Manually construct cyclic nodes
	n1 := &Node{ID: "n1", ParentID: "n2"}
	n2 := &Node{ID: "n2", ParentID: "n1"}
	g.Nodes["n1"] = n1
	g.Nodes["n2"] = n2

	_, err := g.GetRootOf("n1")
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle detected error, got %v", err)
	}
}

