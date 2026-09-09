package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractContentToolCalls_GemmaFormat(t *testing.T) {
	raw := `Ah, thank you for providing the necessary correction in the topological map.

Let us proceed to inspect the contents of the internal/ directory.

<call>list_directory{path:<|"|>internal<|"|>}</call>`

	cleaned, calls := ExtractContentToolCalls(raw)

	if len(calls) != 1 {
		t.Fatalf("expected 1 extracted tool call, got %d", len(calls))
	}

	if calls[0].Function.Name != "list_directory" {
		t.Errorf("expected tool name 'list_directory', got: %s", calls[0].Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(calls[0].Function.Arguments, &args); err != nil {
		t.Fatalf("failed to parse arguments JSON: %v", err)
	}

	if args["path"] != "internal" {
		t.Errorf("expected path 'internal', got: %v", args["path"])
	}

	if strings.Contains(cleaned, "<call>") || strings.Contains(cleaned, "</call>") {
		t.Errorf("expected <call> block to be stripped from cleaned content, got:\n%s", cleaned)
	}
}

func TestExtractContentToolCalls_ToolCodeFormat(t *testing.T) {
	raw := `I shall now inspect the definition and constraints of the execution contracts.

tool_code: read_file{path: "internal/engine/service.go"} 🛠️💻`

	cleaned, calls := ExtractContentToolCalls(raw)

	if len(calls) != 1 {
		t.Fatalf("expected 1 extracted tool call, got %d", len(calls))
	}

	if calls[0].Function.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got: %s", calls[0].Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(calls[0].Function.Arguments, &args); err != nil {
		t.Fatalf("failed to parse arguments JSON: %v", err)
	}

	if args["path"] != "internal/engine/service.go" {
		t.Errorf("expected path 'internal/engine/service.go', got: %v", args["path"])
	}

	if strings.Contains(cleaned, "tool_code:") {
		t.Errorf("expected tool_code: to be stripped, got:\n%s", cleaned)
	}
}

func TestExtractContentToolCalls_NoToolCalls(t *testing.T) {
	raw := "Just a normal assistant response with no tools."
	cleaned, calls := ExtractContentToolCalls(raw)
	if len(calls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(calls))
	}
	if cleaned != raw {
		t.Errorf("expected content unchanged, got: %s", cleaned)
	}
}
