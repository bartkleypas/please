package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func TestReadFile_PaginationAndWindowing(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create a 20-line test file with one extra-long line
	var content strings.Builder
	for i := 1; i <= 20; i++ {
		if i == 10 {
			// Super long line (3000 chars)
			content.WriteString(strings.Repeat("longline-", 300) + "\n")
		} else {
			fmt.Fprintf(&content, "Line %d content\n", i)
		}
	}
	if err := os.WriteFile(testFile, []byte(content.String()), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mgr := NewManager(NewGraph(), nil)
	mgr.RegisterDefaultTools(tmpDir)

	// 1. Test basic read with pagination default
	res, err := mgr.Registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if !strings.Contains(res, "[Lines 1-20 of 20") {
		t.Errorf("expected header with 20 lines, got:\n%s", res)
	}
	if !strings.Contains(res, "Line 1 content") {
		t.Errorf("expected Line 1 content in output")
	}
	if !strings.Contains(res, "[line truncated,") {
		t.Errorf("expected long line to be truncated, got:\n%s", res)
	}

	// 2. Test offset and limit
	res, err = mgr.Registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path":   "test.txt",
		"offset": 5,
		"limit":  3,
	})
	if err != nil {
		t.Fatalf("read_file with offset/limit failed: %v", err)
	}
	if !strings.Contains(res, "[Lines 5-7 of 20") {
		t.Errorf("expected slice [Lines 5-7 of 20, got:\n%s", res)
	}
	if !strings.Contains(res, "Line 5 content") || !strings.Contains(res, "Line 7 content") {
		t.Errorf("expected lines 5 through 7, got:\n%s", res)
	}
	if strings.Contains(res, "Line 4 content") || strings.Contains(res, "Line 8 content") {
		t.Errorf("should not contain lines outside slice, got:\n%s", res)
	}
	if !strings.Contains(res, "offset=8") {
		t.Errorf("expected next offset hint (offset=8), got:\n%s", res)
	}

	// 3. Test byte budget
	res, err = mgr.Registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path":      "test.txt",
		"offset":    1,
		"max_bytes": 50,
	})
	if err != nil {
		t.Fatalf("read_file with max_bytes failed: %v", err)
	}
	if !strings.Contains(res, "Byte budget reached") {
		t.Errorf("expected byte budget hint, got:\n%s", res)
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
