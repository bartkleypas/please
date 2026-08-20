package engine

import (
	"context"
	"encoding/json"
)

// Tool defines an external function that the LLM can call
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema for the tool's arguments
	Function    func(ctx context.Context, args map[string]interface{}) (string, error)
	Interactive bool `json:"interactive"` // If true, requires user approval before execution
}

// ToolCall represents a specific request from the LLM to run a tool
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // Usually "function"
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"` // Raw bytes of arguments
	} `json:"function"`
}

// ToolRegistry maintains a collection of available tools
type ToolRegistry struct {
	Tools map[string]Tool
}

// NewToolRegistry creates a new registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		Tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *ToolRegistry) Register(t Tool) {
	r.Tools[t.Name] = t
}

// GetTools returns a slice of all registered tools
func (r *ToolRegistry) GetTools() []Tool {
	tools := make([]Tool, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, t)
	}
	return tools
}
