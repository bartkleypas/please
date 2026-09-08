package engine

import (
	"github.com/bartkleypas/please/internal/graph"
)

// Graph manages the collection of conversation nodes (forwarded from internal/graph)
type Graph = graph.Graph

var (
	// ErrNodeNotFound is returned when a requested node is missing from the graph
	ErrNodeNotFound = graph.ErrNodeNotFound

	// NewGraph initializes a new conversation graph
	NewGraph = graph.NewGraph
)
