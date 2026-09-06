package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolRegistry(t *testing.T) {
	registry := NewToolRegistry()
	tool := Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "success", nil
		},
	}

	registry.Register(tool)

	if _, ok := registry.Tools["test_tool"]; !ok {
		t.Errorf("expected test_tool to be registered")
	}

	tools := registry.GetTools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

func TestToolRegistry_Execute(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(Tool{
		Name: "echo",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			msg, _ := args["msg"].(string)
			return msg, nil
		},
	})

	rawArgs := json.RawMessage(`{"msg": "hello world"}`)
	res, err := registry.Execute(context.Background(), "echo", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", res)
	}

	// Missing tool
	_, err = registry.Execute(context.Background(), "nonexistent", rawArgs)
	if err == nil {
		t.Fatalf("expected error for nonexistent tool, got nil")
	}
}

func TestToolRegistry_DeterministicOrdering(t *testing.T) {
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, "/tmp")

	tools := registry.GetTools()
	if len(tools) == 0 {
		t.Fatalf("expected registered tools, got 0")
	}

	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	// Run multiple times to verify 100% deterministic sequence
	for run := 0; run < 10; run++ {
		subsequent := registry.GetTools()
		for i, tool := range subsequent {
			if tool.Name != names[i] {
				t.Fatalf("iteration %d: tool order mismatch at index %d: expected %s, got %s",
					run, i, names[i], tool.Name)
			}
		}
	}

	// Verify Sensory precedes Mutate precedes Execute
	lastCategoryWeight := -1
	for _, tool := range tools {
		w := categoryPriority(tool.Category)
		if w < lastCategoryWeight {
			t.Errorf("category ordering inverted: tool %s (category %s, weight %d) appeared after weight %d",
				tool.Name, tool.Category, w, lastCategoryWeight)
		}
		lastCategoryWeight = w
	}
}

func TestToolCategory_TaxonomyAndPolicyFiltering(t *testing.T) {
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, "/tmp")

	// Standard/Permissive returns all tools including execute_command
	stdTools := registry.GetToolsForPolicy(SandboxPolicyStandard)
	hasExec := false
	for _, tool := range stdTools {
		if tool.Name == "execute_command" {
			hasExec = true
			if tool.Category != CategoryExecute {
				t.Errorf("expected execute_command to have CategoryExecute, got %s", tool.Category)
			}
		}
	}
	if !hasExec {
		t.Errorf("expected standard policy to include execute_command")
	}

	// Strict drops CategoryExecute tools
	strictTools := registry.GetToolsForPolicy(SandboxPolicyStrict)
	for _, tool := range strictTools {
		if tool.Category == CategoryExecute || tool.Name == "execute_command" {
			t.Errorf("strict policy leaked execution tool: %s (%s)", tool.Name, tool.Category)
		}
	}
}
