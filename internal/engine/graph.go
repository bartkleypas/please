package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrNodeNotFound = errors.New("node not found in graph")
)

// Graph manages the collection of conversation nodes
type Graph struct {
	Nodes map[string]*Node
}

// NewGraph initializes a new conversation graph
func NewGraph() *Graph {
	return &Graph{
		Nodes: make(map[string]*Node),
	}
}

// AddNode inserts a node into the graph
func (g *Graph) AddNode(node *Node) {
	g.Nodes[node.ID] = node
}

// GetNode retrieves a node by its ID
func (g *Graph) GetNode(id string) (*Node, error) {
	node, ok := g.Nodes[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}
	return node, nil
}

// FindNodeByPrefix searches for the first node whose ID starts with the given prefix.
func (g *Graph) FindNodeByPrefix(prefix string) (*Node, error) {
	if prefix == "" {
		return nil, fmt.Errorf("prefix cannot be empty")
	}

	for _, node := range g.Nodes {
		if strings.HasPrefix(node.ID, prefix) {
			return node, nil
		}
	}

	return nil, fmt.Errorf("%w: no node starts with %s", ErrNodeNotFound, prefix)
}

// GetPath returns the linear sequence of nodes from the root to the specified node ID.
// This is used to provide the "current timeline" context to the LLM.
func (g *Graph) GetPath(nodeID string) ([]*Node, error) {
	var path []*Node
	currentID := nodeID

	for currentID != "" {
		node, ok := g.Nodes[currentID]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, currentID)
		}
		path = append(path, node)
		currentID = node.ParentID
	}

	// The path is collected from leaf to root, so we must reverse it
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path, nil
}

// GetChildren returns all nodes that have the specified nodeID as their parent, sorted by timestamp.
func (g *Graph) GetChildren(parentID string) []*Node {
	var children []*Node
	for _, node := range g.Nodes {
		if node.ParentID == parentID {
			children = append(children, node)
		}
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].Timestamp.Before(children[j].Timestamp)
	})

	return children
}

// GetRoots returns all nodes that have no parent, sorted by timestamp.
func (g *Graph) GetRoots() []*Node {
	var roots []*Node
	for _, node := range g.Nodes {
		if node.ParentID == "" {
			roots = append(roots, node)
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Timestamp.Before(roots[j].Timestamp)
	})

	return roots
}

// GetSystemRoot retrieves the first root node and verifies it is a system prompt.
func (g *Graph) GetSystemRoot() (*Node, error) {
	roots := g.GetRoots()
	if len(roots) == 0 {
		return nil, fmt.Errorf("no root node found")
	}
	root := roots[0]
	if root.Role != RoleSystem {
		return nil, fmt.Errorf("root node is not a system prompt")
	}
	return root, nil
}
