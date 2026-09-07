package providers

import (
	"context"
	"encoding/json"

	"github.com/bartkleypas/please/internal/tools"
)

// Role defines the speaker or category of a message turn.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSummary   Role = "summary"
)

// ToolCall represents an invokable action request emitted by an LLM backend.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // Usually "function"
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// ToolObservation represents the result of a tool execution fed back to the LLM.
type ToolObservation struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
}

// Message represents an individual turn or prompt segment prepared for an LLM provider.
type Message struct {
	ID           string            `json:"id,omitempty"`
	ParentID     string            `json:"parent_id,omitempty"`
	Role         Role              `json:"role"`
	Content      string            `json:"content"`
	Thought      string            `json:"thought,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	Observations []ToolObservation `json:"observations,omitempty"`
	Internal     bool              `json:"internal,omitempty"`
	Images       []string          `json:"images,omitempty"`
}

// Provider defines the interface for interacting with different AI model backends.
type Provider interface {
	GenerateResponse(ctx context.Context, messages []Message, availableTools []tools.Tool) (*Message, error)
	GenerateResponseStream(ctx context.Context, messages []Message, availableTools []tools.Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error)
}

// LLMProvider is an alias for Provider for backward compatibility.
type LLMProvider = Provider
