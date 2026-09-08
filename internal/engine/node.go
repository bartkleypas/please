package engine

import (
	"github.com/bartkleypas/please/internal/graph"
)

// Re-export provider and graph types for backward compatibility
type Role = graph.Role

const (
	RoleSystem    = graph.RoleSystem
	RoleUser      = graph.RoleUser
	RoleAssistant = graph.RoleAssistant
	RoleTool      = graph.RoleTool
	RoleSummary   = graph.RoleSummary
)

// ToolObservation represents the result of a side-channel tool execution
type ToolObservation = graph.ToolObservation

// Node represents a single point in the conversation graph
type Node = graph.Node
