package engine

import "time"

// Role defines the speaker of the message
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSummary   Role = "summary"
)

// ToolObservation represents the result of a side-channel tool execution
type ToolObservation struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
}

// Node represents a single point in the conversation graph
type Node struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parent_id"` // Empty if root
	Role      Role              `json:"role"`
	Content   string            `json:"content"`
	Thought   string            `json:"thought,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Images    []string          `json:"images,omitempty"`

	// Tool handling fields
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`  // Present if Role == Assistant
	ToolCallID   string            `json:"tool_call_id,omitempty"` // Present if Role == Tool
	Observations []ToolObservation `json:"observations,omitempty"` // Side-channel results

	// Deletion state
	Deleted bool `json:"deleted,omitempty"`

	Internal bool `json:"internal,omitempty"`

	// Encryption state
	Encrypted bool `json:"-"` // Not persisted directly to JSON/DB; computed on load/save
}
