package engine

import (
	"context"
	"encoding/json"
	"strings"
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

func TestRoleSummary_ProviderMapping(t *testing.T) {
	summaryMsg := Message{
		Role:    RoleSummary,
		Content: "This is a compacted milestone summary.",
	}

	// Test Ollama message mapping
	ollamaMsgs := mapToOllamaMessages([]Message{summaryMsg})
	if len(ollamaMsgs) != 1 {
		t.Fatalf("expected 1 ollama message, got %d", len(ollamaMsgs))
	}
	if ollamaMsgs[0].Role != "system" {
		t.Errorf("expected mapped role 'system', got '%s'", ollamaMsgs[0].Role)
	}
	if !strings.Contains(ollamaMsgs[0].Content, "This is a compacted milestone summary.") {
		t.Errorf("expected summary content in ollama message")
	}

	// Test OpenAI message mapping
	openAIMsgs := mapToOpenAIMessages([]Message{summaryMsg})
	if len(openAIMsgs) != 1 {
		t.Fatalf("expected 1 openai message, got %d", len(openAIMsgs))
	}
	if openAIMsgs[0].Role != "system" {
		t.Errorf("expected mapped role 'system', got '%s'", openAIMsgs[0].Role)
	}
	if !strings.Contains(openAIMsgs[0].Content.(string), "This is a compacted milestone summary.") {
		t.Errorf("expected summary content in openai message")
	}
}
