// package engine provides the core business logic for the Please application,
// including DAG management, LLM provider integration, and state persistence.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Manager is the central coordinator for the application engine. It provides
// a high-level API that combines graph operations (traversal, branching)
// with storage persistence, ensuring that all narrative changes are saved.
type Manager struct {
	Graph    *Graph
	Storage  Storage
	Registry *ToolRegistry
}

// NewManager creates a new Manager instance
func NewManager(g *Graph, s Storage) *Manager {
	return &Manager{
		Graph:    g,
		Storage:  s,
		Registry: NewToolRegistry(),
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

// CreateAssistantNode creates a node for the assistant, potentially containing tool calls
func (m *Manager) CreateAssistantNode(parentID string, content string, toolCalls []ToolCall) (*Node, error) {
	node := &Node{
		ID:        uuid.NewString(),
		ParentID:  parentID,
		Role:      RoleAssistant,
		Content:   content,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist assistant node: %w", err)
	}

	return node, nil
}

// CreateToolNode creates a node containing the result of a tool execution
func (m *Manager) CreateToolNode(parentID string, toolCallID string, content string) (*Node, error) {
	node := &Node{
		ID:         uuid.NewString(),
		ParentID:   parentID,
		Role:       RoleTool,
		Content:    content,
		Timestamp:  time.Now(),
		ToolCallID: toolCallID,
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist tool node: %w", err)
	}

	return node, nil
}

// ExecuteToolCall runs the function associated with a tool call
func (m *Manager) ExecuteToolCall(ctx context.Context, call ToolCall) (string, error) {
	tool, ok := m.Registry.Tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", call.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	return tool.Function(ctx, args)
}

// Sync reloads the graph from storage, effectively synchronizing the in-memory state
// with any external changes (e.g., from other 'please' sessions).
func (m *Manager) Sync() (*Graph, string, error) {
	graph, lastID, err := m.Storage.LoadGraph()
	if err != nil {
		return nil, "", err
	}
	m.Graph = graph
	return graph, lastID, nil
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

// PruneBranch recursively flags a node and all its descendants as deleted
func (m *Manager) PruneBranch(nodeID string) error {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		return err
	}

	// Recursive helper to flag and persist
	var flagDeleted func(n *Node) error
	flagDeleted = func(n *Node) error {
		n.Deleted = true
		if err := m.Storage.UpdateNodeMetadata(n); err != nil {
			return err
		}

		children := m.GetChildren(n.ID)
		for _, child := range children {
			if err := flagDeleted(child); err != nil {
				return err
			}
		}
		return nil
	}

	if err := flagDeleted(node); err != nil {
		return err
	}

	// Refresh in-memory graph to reflect deletions
	_, _, err = m.Sync()
	return err
}

// GarbageCollect permanently removes flagged nodes from storage and reloads the graph
func (m *Manager) GarbageCollect() (int64, error) {
	count, err := m.Storage.GarbageCollect()
	if err != nil {
		return count, err
	}

	_, _, err = m.Sync()
	return count, err
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
