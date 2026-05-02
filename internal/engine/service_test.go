package engine

import (
	"strings"
	"testing"
)

func TestManager_Validation(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	tests := []struct {
		name    string
		fn      func() error
		wantErr string
	}{
		{
			name: "Empty user content",
			fn: func() error {
				_, err := mgr.CreateNode("root", RoleUser, "", false)
				return err
			},
			wantErr: "user message content cannot be empty",
		},
		{
			name: "Self-parenting node",
			fn: func() error {
				// We have to bypass the UUID generation to test ID == ParentID
				// Since CreateNode generates a UUID, we'll test the internal validateNode directly
				node := &Node{ID: "A", ParentID: "A", Role: RoleUser, Content: "Hello"}
				return mgr.validateNode(node)
			},
			wantErr: "node cannot be its own parent",
		},
		{
			name: "Tool node missing ToolCallID",
			fn: func() error {
				_, err := mgr.CreateToolNode("root", "", "result", false)
				return err
			},
			wantErr: "tool node must have a ToolCallID",
		},
		{
			name: "Tool node empty content",
			fn: func() error {
				_, err := mgr.CreateToolNode("root", "call_123", "", false)
				return err
			},
			wantErr: "tool result content cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Error("expected error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// MockStorage for testing
type MockStorage struct{}

func (s *MockStorage) SaveNode(n *Node) error            { return nil }
func (s *MockStorage) LoadGraph() (*Graph, string, error) { return NewGraph(), "", nil }
func (s *MockStorage) UpdateNodeMetadata(n *Node) error  { return nil }
func (s *MockStorage) UpdateNodeParentID(id, p string) error { return nil }
func (s *MockStorage) UpdateNodeObservations(id string, obs []ToolObservation) error { return nil }
func (s *MockStorage) GarbageCollect() (int64, error)   { return 0, nil }
func (s *MockStorage) Close() error                     { return nil }
func (s *MockStorage) Vacuum() error                    { return nil }
