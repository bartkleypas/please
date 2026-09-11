package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalHarnessProvider_MultiTurnStream(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name:        "get_time",
		Description: "Get current time",
		Parameters: map[string]interface{}{
			"type": "object",
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "12:00 PM UTC", nil
		},
	})

	callCount := 0
	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			callCount++
			if callCount == 1 {
				return "", "Checking clock...", []ToolCall{
					{
						ID:   "call_clock_1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "get_time",
							Arguments: json.RawMessage(`{}`),
						},
					},
				}, nil
			}
			return "The time is 12:00 PM UTC. 🦉☕", "Synthesizing", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)
	localProvider := NewLocalHarnessProvider(harness, "alpha")

	userNode, _ := mgr.CreateNode("", RoleUser, "What time is it?", false)

	contentChan, thoughtChan, toolCallChan, errChan := localProvider.GenerateResponseStream(
		context.Background(),
		[]Message{{Role: RoleUser, Content: "What time is it?", ID: userNode.ID}},
		nil,
	)

	var thoughts strings.Builder
	var content strings.Builder

	for contentChan != nil || thoughtChan != nil || toolCallChan != nil || errChan != nil {
		select {
		case chunk, ok := <-contentChan:
			if !ok {
				contentChan = nil
			} else {
				content.WriteString(chunk)
			}
		case thought, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
			} else {
				thoughts.WriteString(thought)
			}
		case _, ok := <-toolCallChan:
			if !ok {
				toolCallChan = nil
			} else {
				t.Fatalf("unexpected tool call emitted on toolCallChan; harness should execute tools internally")
			}
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}

	finalContent := content.String()
	finalThoughts := thoughts.String()

	if !strings.Contains(finalContent, "12:00 PM UTC") {
		t.Errorf("expected content to contain '12:00 PM UTC', got: %q", finalContent)
	}

	if !strings.Contains(finalThoughts, "Executing get_time") {
		t.Errorf("expected thought stream to contain tool execution badge, got: %q", finalThoughts)
	}

	if !strings.Contains(finalThoughts, "Result: 12:00 PM UTC") {
		t.Errorf("expected thought stream to contain tool result badge, got: %q", finalThoughts)
	}

	// Verify session head was updated to the assistant node
	headID, err := storage.GetSessionHead("alpha")
	if err != nil || headID == "" {
		t.Fatalf("expected session head for 'alpha' to be set")
	}

	headNode, err := mgr.GetNode(headID)
	if err != nil || headNode == nil {
		t.Fatalf("failed to retrieve head node: %v", err)
	}

	if len(headNode.ToolCalls) != 1 || headNode.ToolCalls[0].Function.Name != "get_time" {
		t.Errorf("expected head node to have 1 tool call for 'get_time', got: %v", headNode.ToolCalls)
	}

	if len(headNode.Observations) != 1 || !strings.Contains(headNode.Observations[0].Result, "12:00 PM UTC") {
		t.Errorf("expected head node to have 1 observation with '12:00 PM UTC', got: %v", headNode.Observations)
	}
}

func TestLocalHarnessProvider_GenerateResponse_StatelessOneShot(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mockProvider := &MockLLMProvider{
		ResponseContent: "Stateless summary paragraph",
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)
	localProvider := NewLocalHarnessProvider(harness, "main")

	// RawProvider should expose the underlying mock provider
	if localProvider.RawProvider() != mockProvider {
		t.Errorf("expected RawProvider to return underlying mock provider")
	}

	// 1. Initial graph has 0 nodes
	if len(graph.Nodes) != 0 {
		t.Fatalf("expected 0 initial graph nodes, got %d", len(graph.Nodes))
	}

	// 2. Execute one-shot GenerateResponse (e.g. as used in /compact)
	messages := []Message{
		{Role: RoleSystem, Content: "You are an archivist."},
		{Role: RoleUser, Content: "Summarize this segment."},
	}

	resp, err := localProvider.GenerateResponse(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}

	if resp.Content != "Stateless summary paragraph" {
		t.Errorf("expected 'Stateless summary paragraph', got: %s", resp.Content)
	}

	// 3. Verify ZERO nodes were created in the graph
	if len(graph.Nodes) != 0 {
		t.Errorf("CRITICAL: GenerateResponse mutated the graph! Expected 0 nodes, got %d", len(graph.Nodes))
	}

	// 4. Verify no session head was created/modified
	headID, _ := storage.GetSessionHead("main")
	if headID != "" {
		t.Errorf("expected empty session head, got %s", headID)
	}
}

