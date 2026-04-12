package engine

import "time"

// Role defines the speaker of the message
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Node represents a single point in the conversation graph
type Node struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parent_id"` // Empty if root
	Role      Role              `json:"role"`
	Content   string            `json:"content"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
