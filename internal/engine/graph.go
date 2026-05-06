package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNodeNotFound = errors.New("node not found in graph")
)

// Graph manages the collection of conversation nodes
type Graph struct {
	Nodes    map[string]*Node
	Children map[string][]string
	Roots    []string
	mu       sync.RWMutex
}

// NewGraph initializes a new conversation graph
func NewGraph() *Graph {
	return &Graph{
		Nodes:    make(map[string]*Node),
		Children: make(map[string][]string),
		Roots:    []string{},
	}
}

// AddNode inserts a node into the graph and maintains sorted order for children and roots.
// Re-sorting by timestamp on every insertion ensures that the DAG always reflects
// a consistent chronological narrative, even if nodes are loaded out of order.
func (g *Graph) AddNode(node *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Nodes[node.ID] = node

	// Track children
	if node.ParentID != "" {
		g.Children[node.ParentID] = append(g.Children[node.ParentID], node.ID)
		// Re-sort children by timestamp to ensure consistent playback/mapping.
		childIDs := g.Children[node.ParentID]
		sort.Slice(childIDs, func(i, j int) bool {
			nodeI, okI := g.Nodes[childIDs[i]]
			nodeJ, okJ := g.Nodes[childIDs[j]]
			if !okI || !okJ {
				return false
			}
			return nodeI.Timestamp.Before(nodeJ.Timestamp)
		})
	} else {
		// Track roots
		g.Roots = append(g.Roots, node.ID)
		// Re-sort roots by timestamp to handle multi-persona initializations.
		sort.Slice(g.Roots, func(i, j int) bool {
			nodeI, okI := g.Nodes[g.Roots[i]]
			nodeJ, okJ := g.Nodes[g.Roots[j]]
			if !okI || !okJ {
				return false
			}
			return nodeI.Timestamp.Before(nodeJ.Timestamp)
		})
	}
}

// GetNode retrieves a node by its ID
func (g *Graph) GetNode(id string) (*Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	node, ok := g.Nodes[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}
	return node, nil
}

// FindNodeByShortID searches for the first node whose ID starts or ends with the given string.
func (g *Graph) FindNodeByShortID(shortID string) (*Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if shortID == "" {
		return nil, fmt.Errorf("shortID cannot be empty")
	}

	for _, node := range g.Nodes {
		if strings.HasPrefix(node.ID, shortID) || strings.HasSuffix(node.ID, shortID) {
			return node, nil
		}
	}

	return nil, fmt.Errorf("%w: no node matches %s", ErrNodeNotFound, shortID)
}

// GetPath returns the linear sequence of nodes from the root to the specified node ID.
// This is used to provide the "current timeline" context to the LLM. It performs
// a two-pass traversal: first to determine depth, and then to collect nodes from
// leaf to root. The final slice is reversed to provide a chronological history.
func (g *Graph) GetPath(nodeID string) ([]*Node, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var path []*Node
	currentID := nodeID

	// First, traverse to find the depth/length of the path and detect cycles
	depth := 0
	tempID := nodeID
	var previousID string

	for tempID != "" {
		if previousID != "" && tempID >= previousID {
			return nil, fmt.Errorf("chronological cycle detected: parent %s is newer than or equal to child %s", tempID, previousID)
		}

		node, ok := g.Nodes[tempID]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, tempID)
		}
		depth++
		previousID = tempID
		tempID = node.ParentID
	}

	// Pre-allocate the slice with the known depth
	path = make([]*Node, 0, depth)
	currentID = nodeID

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
	g.mu.RLock()
	defer g.mu.RUnlock()

	childIDs, ok := g.Children[parentID]
	if !ok {
		return []*Node{}
	}

	var children []*Node
	for _, id := range childIDs {
		if node, ok := g.Nodes[id]; ok {
			children = append(children, node)
		}
	}

	return children
}

// GetRoots returns all nodes that have no parent, sorted by timestamp.
func (g *Graph) GetRoots() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var roots []*Node
	for _, id := range g.Roots {
		if node, ok := g.Nodes[id]; ok {
			roots = append(roots, node)
		}
	}

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

