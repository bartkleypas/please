package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bartkleypas/please/internal/domain"
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

func TestToolRegistry_Dispatch(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(Tool{
		Name: "echo",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			msg, _ := args["msg"].(string)
			return msg, nil
		},
	})

	rawArgs := json.RawMessage(`{"msg": "hello world"}`)
	res, err := registry.Dispatch(context.Background(), "echo", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", res)
	}

	// Missing tool
	_, err = registry.Dispatch(context.Background(), "nonexistent", rawArgs)
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

	// 1. Permissive returns all tools including execute_command
	permTools := registry.GetToolsForPolicy(string(domain.SandboxPolicyPermissive))
	hasExec := false
	for _, tool := range permTools {
		if tool.Name == "execute_command" {
			hasExec = true
			if tool.Category != domain.CategoryExecute {
				t.Errorf("expected execute_command to have CategoryExecute, got %s", tool.Category)
			}
		}
	}
	if !hasExec {
		t.Errorf("expected permissive policy to include execute_command")
	}

	// 2. Standard policy includes Mutate & Sensory, but drops execute_command
	stdTools := registry.GetToolsForPolicy(string(domain.SandboxPolicyStandard))
	hasMutate := false
	for _, tool := range stdTools {
		if tool.Category == domain.CategoryExecute || tool.Name == "execute_command" {
			t.Errorf("standard policy leaked execution tool: %s (%s)", tool.Name, tool.Category)
		}
		if tool.Category == domain.CategoryMutate {
			hasMutate = true
		}
	}
	if !hasMutate {
		t.Errorf("expected standard policy to include mutate tools")
	}

	// 3. Strict drops BOTH CategoryExecute and CategoryMutate tools (Sensory only)
	strictTools := registry.GetToolsForPolicy(string(domain.SandboxPolicyStrict))
	for _, tool := range strictTools {
		if tool.Category == domain.CategoryExecute || tool.Name == "execute_command" {
			t.Errorf("strict policy leaked execution tool: %s (%s)", tool.Name, tool.Category)
		}
		if tool.Category == domain.CategoryMutate {
			t.Errorf("strict policy leaked mutate tool: %s (%s)", tool.Name, tool.Category)
		}
		if tool.Category != domain.CategorySensory {
			t.Errorf("strict policy allowed non-sensory tool: %s (%s)", tool.Name, tool.Category)
		}
	}
	if len(strictTools) == 0 {
		t.Errorf("expected strict policy to retain sensory tools, got 0")
	}
}
