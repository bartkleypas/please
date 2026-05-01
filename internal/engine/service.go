// package engine provides the core business logic for the Please application,
// including DAG management, LLM provider integration, and state persistence.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
func (m *Manager) CreateNode(parentID string, role Role, content string, internal bool) (*Node, error) {
	node := &Node{
		ID:        uuid.NewString(),
		ParentID:  parentID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Internal:  internal,
	}

	if err := m.validateNode(node); err != nil {
		return nil, fmt.Errorf("node validation failed: %w", err)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist new node: %w", err)
	}

	return node, nil
}

// CreateAssistantNode creates a node for the assistant, potentially containing tool calls and reasoning
func (m *Manager) CreateAssistantNode(parentID string, content string, thought string, toolCalls []ToolCall, internal bool) (*Node, error) {
	node := &Node{
		ID:        uuid.NewString(),
		ParentID:  parentID,
		Role:      RoleAssistant,
		Content:   content,
		Thought:   thought,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
		Internal:  internal,
	}

	if err := m.validateNode(node); err != nil {
		return nil, fmt.Errorf("assistant node validation failed: %w", err)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist assistant node: %w", err)
	}

	return node, nil
}

// CreateToolNode creates a node containing the result of a tool execution
func (m *Manager) CreateToolNode(parentID string, toolCallID string, content string, internal bool) (*Node, error) {
	node := &Node{
		ID:         uuid.NewString(),
		ParentID:   parentID,
		Role:       RoleTool,
		Content:    content,
		Timestamp:  time.Now(),
		ToolCallID: toolCallID,
		Internal:   internal,
	}

	if err := m.validateNode(node); err != nil {
		return nil, fmt.Errorf("tool node validation failed: %w", err)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist tool node: %w", err)
	}

	return node, nil
}

func (m *Manager) validateNode(node *Node) error {
	if node.ID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}
	if node.ID == node.ParentID {
		return fmt.Errorf("node cannot be its own parent (cycle detected)")
	}
	if node.Role == RoleUser && strings.TrimSpace(node.Content) == "" {
		return fmt.Errorf("user message content cannot be empty")
	}
	if node.Role == RoleTool {
		if node.ToolCallID == "" {
			return fmt.Errorf("tool node must have a ToolCallID")
		}
		if strings.TrimSpace(node.Content) == "" {
			return fmt.Errorf("tool result content cannot be empty")
		}
	}
	return nil
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

// CompactRange summarizes a set of nodes and grafts them into the graph as a Supernode
func (m *Manager) CompactRange(ctx context.Context, provider LLMProvider, nodeIDs []string) (*Node, error) {
	if len(nodeIDs) == 0 {
		return nil, fmt.Errorf("no nodes provided for compaction")
	}

	var contentToSummarize strings.Builder
	for _, id := range nodeIDs {
		node, err := m.Graph.GetNode(id)
		if err != nil {
			continue
		}
		fmt.Fprintf(&contentToSummarize, "[%s]: %s\n", node.Role, node.Content)
	}

	// 1. Generate Summary
	summaryPrompt := "You are a concise narrative archivist. Summarize the following conversation segment into a single, high-density paragraph. Preserve key facts, decisions, and the current state of the world. Do not use filler or introductory phrases."
	messages := []Message{
		{Role: RoleSystem, Content: summaryPrompt},
		{Role: RoleUser, Content: contentToSummarize.String()},
	}

	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// 2. Determine Parentage
	firstNode, err := m.Graph.GetNode(nodeIDs[0]) // Assumes IDs are in chronological order
	if err != nil {
		return nil, fmt.Errorf("failed to find first node in range: %w", err)
	}
	parentID := firstNode.ParentID

	// 3. Create Supernode
	superNode, err := m.createSupernode(parentID, resp.Content)
	if err != nil {
		return nil, err
	}

	// 4. Graft children of the LAST node in the range onto the Supernode
	lastNodeID := nodeIDs[len(nodeIDs)-1]
	children := m.Graph.GetChildren(lastNodeID)
	for _, child := range children {
		if err := m.Storage.UpdateNodeParentID(child.ID, superNode.ID); err != nil {
			return nil, fmt.Errorf("failed to re-parent child %s: %w", child.ID, err)
		}
	}

	// 5. Sync to reflect structural changes
	_, _, err = m.Sync()
	return superNode, err
}

func (m *Manager) createSupernode(parentID string, content string) (*Node, error) {
	node := &Node{
		ID:        uuid.NewString(),
		ParentID:  parentID,
		Role:      RoleSummary,
		Content:   content,
		Timestamp: time.Now(),
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist supernode: %w", err)
	}

	return node, nil
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
