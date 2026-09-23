package graph

import (
	"time"

	"github.com/bartkleypas/please/internal/domain"
)

// Re-export domain types for backward compatibility across graph callers
type Role = domain.Role

const (
	RoleSystem    = domain.RoleSystem
	RoleUser      = domain.RoleUser
	RoleAssistant = domain.RoleAssistant
	RoleTool      = domain.RoleTool
	RoleSummary   = domain.RoleSummary
)

// ToolCall represents a tool invocation requested by the model
type ToolCall = domain.ToolCall

// ToolObservation represents the result of a side-channel tool execution
type ToolObservation = domain.ToolObservation

// Node represents a single point in the conversation graph
type Node struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parent_id"` // Empty if root
	Role      domain.Role       `json:"role"`
	Content   string            `json:"content"`
	Thought   string            `json:"thought,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Images    []string          `json:"images,omitempty"`

	// Tool handling fields
	ToolCalls    []domain.ToolCall        `json:"tool_calls,omitempty"`   // Present if Role == Assistant
	ToolCallID   string                   `json:"tool_call_id,omitempty"` // Present if Role == Tool
	Observations []domain.ToolObservation `json:"observations,omitempty"` // Side-channel results

	// Deletion state
	Deleted bool `json:"deleted,omitempty"`

	Internal bool `json:"internal,omitempty"`

	// Encryption state
	Encrypted bool `json:"-"` // Not persisted directly to JSON/DB; computed on load/save
}
