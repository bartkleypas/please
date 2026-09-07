package engine

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

func TestExecuteToolCall(t *testing.T) {
	mgr := NewManager(NewGraph(), nil)
	mgr.Registry.Register(Tool{
		Name: "echo",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return args["msg"].(string), nil
		},
	})

	call := ToolCall{
		ID: "123",
		Function: struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}{
			Name:      "echo",
			Arguments: json.RawMessage(`{"msg": "hello"}`),
		},
	}

	result, err := mgr.ExecuteToolCall(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "hello" {
		t.Errorf("expected 'hello', got '%s'", result)
	}
}

func TestManager_RegisterDefaultTools(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(NewGraph(), nil)
	mgr.RegisterDefaultTools(tmpDir)

	tools := mgr.Registry.GetTools()
	if len(tools) == 0 {
		t.Fatalf("expected registered default tools, got 0")
	}

	if _, ok := mgr.Registry.Tools["read_file"]; !ok {
		t.Errorf("expected read_file in manager tool registry")
	}
}
