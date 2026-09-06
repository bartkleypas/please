package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolCategory defines the fundamental nature of a tool's capability.
type ToolCategory string

const (
	CategorySensory ToolCategory = "sensory" // Read-only / discovery
	CategoryMutate  ToolCategory = "mutate"  // State-modifying / writes
	CategoryExecute ToolCategory = "execute" // Host compute execution
)

// Tool defines an external function that the LLM can call.
type Tool struct {
	Name        string       `json:"name"`
	Category    ToolCategory `json:"category,omitempty"`
	Description string       `json:"description"`
	Parameters  interface{}  `json:"parameters"` // JSON Schema for the tool's arguments
	Function    func(ctx context.Context, args map[string]interface{}) (string, error)
	Interactive bool         `json:"interactive"` // If true, requires user approval before execution
}

// ToolRegistry maintains a collection of available tools.
type ToolRegistry struct {
	Tools map[string]Tool
}

// NewToolRegistry creates a new empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		Tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(t Tool) {
	r.Tools[t.Name] = t
}

// Execute parses raw JSON arguments and invokes the named tool.
func (r *ToolRegistry) Execute(ctx context.Context, name string, rawArgs json.RawMessage) (string, error) {
	tool, ok := r.Tools[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}

	var args map[string]interface{}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	} else {
		args = make(map[string]interface{})
	}

	return tool.Function(ctx, args)
}

// categoryPriority maps ToolCategory to deterministic sequence weights.
// Follows the Unix paradigm: Sensory/Read -> Mutate/Write -> Execute/Verify.
func categoryPriority(c ToolCategory) int {
	switch c {
	case CategorySensory:
		return 10
	case CategoryMutate:
		return 20
	case CategoryExecute:
		return 30
	default:
		return 99
	}
}

// toolFamilyPriority provides backwards-compatible name-based priority fallback.
var toolFamilyPriority = map[string]int{
	"read_file":            10,
	"list_directory":       11,
	"list_files_recursive": 12,
	"grep_search":          13,
	"write_file":           20,
	"append_file":          21,
	"edit_file":            22,
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
		pi := categoryPriority(tools[i].Category)
		pj := categoryPriority(tools[j].Category)
		if pi == 99 && toolFamilyPriority[tools[i].Name] > 0 {
			pi = toolFamilyPriority[tools[i].Name]
		}
		if pj == 99 && toolFamilyPriority[tools[j].Name] > 0 {
			pj = toolFamilyPriority[tools[j].Name]
		}
		if pi != pj {
			return pi < pj
		}
		return tools[i].Name < tools[j].Name
	})
	return tools
}

// GetToolsForPolicy returns tools filtered and ordered according to the active sandbox policy.
// In SandboxPolicyStrict, execution-class tools are excluded entirely from model context.
func (r *ToolRegistry) GetToolsForPolicy(policy string) []Tool {
	allTools := r.GetTools()
	if strings.ToLower(policy) != SandboxPolicyStrict {
		return allTools
	}
	filtered := make([]Tool, 0, len(allTools))
	for _, t := range allTools {
		if t.Category == CategoryExecute || t.Name == "execute_command" {
			continue // Drop execution tools under strict sandbox policy
		}
		filtered = append(filtered, t)
	}
	return filtered
}
