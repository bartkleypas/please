// package engine provides the core business logic for the Please application,
// including DAG management, LLM provider integration, and state persistence.
package engine

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Manager is the central coordinator for the application engine. It provides
// a high-level API that combines graph operations (traversal, branching)
// with storage persistence, ensuring that all narrative changes are saved.
type Manager struct {
	Graph   *Graph
	Storage Storage
}

// NewManager creates a new Manager instance
func NewManager(g *Graph, s Storage) *Manager {
	return &Manager{
		Graph:   g,
		Storage: s,
	}
}

// CreateNode handles the full lifecycle of creating a new node:
// ID generation, graph insertion, and persistence.
func (m *Manager) CreateNode(parentID string, role Role, content string) (*Node, error) {
	node := &Node{
		ID:        uuid.NewString(),
		ParentID:  parentID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist new node: %w", err)
	}

	return node, nil
}

// SetBookmark updates the bookmark status of a node in its metadata
func (m *Manager) SetBookmark(nodeID string, bookmarked bool) error {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		return err
	}

	if node.Metadata == nil {
		node.Metadata = make(map[string]string)
	}

	if bookmarked {
		node.Metadata["bookmarked"] = "true"
	} else {
		delete(node.Metadata, "bookmarked")
	}

	return nil
}

// Delegation methods to encapsulated Graph operations

func (m *Manager) GetNode(id string) (*Node, error) {
	return m.Graph.GetNode(id)
}

func (m *Manager) FindNodeByPrefix(prefix string) (*Node, error) {
	return m.Graph.FindNodeByPrefix(prefix)
}

func (m *Manager) GetPath(nodeID string) ([]*Node, error) {
	return m.Graph.GetPath(nodeID)
}

func (m *Manager) GetChildren(parentID string) []*Node {
	return m.Graph.GetChildren(parentID)
}

func (m *Manager) GetRoots() []*Node {
	return m.Graph.GetRoots()
}

func (m *Manager) GetSystemRoot() (*Node, error) {
	return m.Graph.GetSystemRoot()
}

func (m *Manager) GetAllNodeIDs() []string {
	ids := make([]string, 0, len(m.Graph.Nodes))
	for id := range m.Graph.Nodes {
		ids = append(ids, id)
	}
	return ids
}
