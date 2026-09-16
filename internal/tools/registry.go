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
	CategorySensory ToolCategory = "sensory" // Read-only / discovery (Should be ordered "first")
	CategoryMutate  ToolCategory = "mutate"  // State-modifying / writes (ordered "second")
	CategoryExecute ToolCategory = "execute" // Host compute execution (ordered "third")
)

// Tool defines an external function that the LLM can call.
type Tool struct {
	Name        string       `json:"name"`
	Category    ToolCategory `json:"category,omitempty"` // Turns out, pretty important now :D
	Description string       `json:"description"`
	Parameters  interface{}  `json:"parameters"` // JSON Schema for the tool's arguments
	Function    func(ctx context.Context, args map[string]interface{}) (string, error)
	Interactive bool `json:"interactive"` // If true, requires user approval before execution
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

// RegisterDefaults populates the registry with standard built-in cybernetic tools scoped to workspaceDir.
// Might change shape now that each tool has its own internal priority?
func (r *ToolRegistry) RegisterDefaults(workspaceDir ...string) {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	for _, t := range SensoryTools(ws, prim) {
		r.Register(t)
	}
	for _, t := range MutateTools(ws, prim) {
		r.Register(t)
	}
	for _, t := range ExecTools(ws) {
		r.Register(t)
	}
}

// RegisterDefaultTools registers all default tools into the provided registry scoped to workspaceDir.
// Also actual surface area in the `engine` package.
func RegisterDefaultTools(registry *ToolRegistry, workspaceDir ...string) {
	registry.RegisterDefaults(workspaceDir...)
}

// GetDefaultTools returns a slice of standard built-in tools scoped to workspaceDir.
func GetDefaultTools(workspaceDir ...string) []Tool {
	reg := NewToolRegistry()
	reg.RegisterDefaults(workspaceDir...)
	return reg.GetTools()
}

// Dispatch parses raw JSON arguments and invokes the named tool.
func (r *ToolRegistry) Dispatch(ctx context.Context, name string, rawArgs json.RawMessage) (string, error) {
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

// GetTools returns a deterministically ordered slice of all registered tools.
// Preserves prompt prefix stability for 100% LLM KV cache reuse across turns.
func (r *ToolRegistry) GetTools() []Tool {
	tools := make([]Tool, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, t)
	}
	// So the below map derivation changes shape too.
	sort.Slice(tools, func(i, j int) bool {
		pi := categoryPriority(tools[i].Category)
		pj := categoryPriority(tools[j].Category)
		if pi != pj {
			return pi < pj
		}
		return tools[i].Name < tools[j].Name
	})
	return tools
}

// GetToolsForPolicy returns tools filtered and ordered according to the active sandbox policy:
// - SandboxPolicyStrict: only CategorySensory tools (read-only perception). Drops Mutate and Execute.
// - SandboxPolicyStandard (default): CategorySensory + CategoryMutate (workspace modifications). Drops Execute.
// - SandboxPolicyPermissive: all tool categories (CategorySensory, CategoryMutate, CategoryExecute).
func (r *ToolRegistry) GetToolsForPolicy(policy string) []Tool {
	allTools := r.GetTools()
	pol := strings.ToLower(strings.TrimSpace(policy))
	if pol == "" {
		pol = SandboxPolicyStandard
	}

	if pol == SandboxPolicyPermissive {
		return allTools
	}

	filtered := make([]Tool, 0, len(allTools))
	for _, t := range allTools {
		switch pol {
		case SandboxPolicyStrict:
			if t.Category == CategorySensory {
				filtered = append(filtered, t)
			}
		case SandboxPolicyStandard:
			fallthrough
		default:
			if t.Category != CategoryExecute && t.Name != "execute_command" {
				filtered = append(filtered, t)
			}
		}
	}
	return filtered
}
