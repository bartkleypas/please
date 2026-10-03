package domain

import "encoding/json"

// Role defines the speaker or category of a message turn.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSummary   Role = "summary"
)

// ToolCategory defines the fundamental nature of a tool's capability.
type ToolCategory string

const (
	CategorySensory ToolCategory = "sensory" // Read-only / discovery
	CategoryMutate  ToolCategory = "mutate"  // State-modifying / writes
	CategoryExecute ToolCategory = "execute" // Host compute execution
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

// SmartReceipt represents an immutable, content-addressed receipt of a tool execution (ADR 021).
type SmartReceipt struct {
	ReceiptID string `json:"receipt_id"`
	Tool      string `json:"tool"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Lines     int    `json:"lines"`
	Bytes     int    `json:"bytes"`
	Summary   string `json:"summary"`
	Banner    string `json:"banner,omitempty"`
	HasBlob   bool   `json:"has_blob"`
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

// ToolSpec represents the schema definition required by LLMs for function calling.
type ToolSpec struct {
	Name        string       `json:"name"`
	Category    ToolCategory `json:"category,omitempty"`
	Description string       `json:"description"`
	Parameters  interface{}  `json:"parameters"`
	Interactive bool         `json:"interactive,omitempty"`
}

// ModelOptions specifies runtime inference parameters for LLM providers.
type ModelOptions struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	TopK             *int     `json:"top_k,omitempty"`
	MinP             *float64 `json:"min_p,omitempty"`
	NumCtx           *int     `json:"num_ctx,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	RepeatPenalty    *float64 `json:"repeat_penalty,omitempty"`
	RepeatLastN      *int     `json:"repeat_last_n,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
}

// SandboxPolicy represents security containment levels.
type SandboxPolicy string

const (
	SandboxPolicyStrict     SandboxPolicy = "strict"
	SandboxPolicyStandard   SandboxPolicy = "standard"
	SandboxPolicyPermissive SandboxPolicy = "permissive"
)
