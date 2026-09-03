package engine

import (
	"context"
	"encoding/json"
	"sort"
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

// toolFamilyPriority defines the deterministic sequence in which tools are presented to the model.
// Follows the Unix paradigm: Sensory/Read -> Mutate/Write -> Execute/Verify.
var toolFamilyPriority = map[string]int{
	// 1. Sensory / Read Family (Inspect reality first)
	"read_file":            10,
	"list_directory":       11,
	"list_files_recursive": 12,
	"search_files":         13,
	"inspect_image":        14,

	// 2. Mutate / Write Family (Act second)
	"write_file":           20,
	"append_file":          21,
	"edit_file":            22,

	// 3. Execution Family (Verify / Run last)
	"execute_command":      30,
}

// GetTools returns a deterministically ordered slice of all registered tools.
// Preserves prompt prefix stability for 100% LLM KV cache reuse across turns.
func (r *ToolRegistry) GetTools() []Tool {
	tools := make([]Tool, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool {
		pi := toolFamilyPriority[tools[i].Name]
		pj := toolFamilyPriority[tools[j].Name]
		if pi == 0 {
			pi = 99 // Uncategorized dynamic tools placed towards the end
		}
		if pj == 0 {
			pj = 99
		}
		if pi != pj {
			return pi < pj
		}
		return tools[i].Name < tools[j].Name
	})
	return tools
}
