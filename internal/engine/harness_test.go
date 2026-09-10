package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSessionHarness_MultiTurnToolExecution(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	// Register a mock tool
	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name:        "get_weather",
		Description: "Get weather for city",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{"type": "string"},
			},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			city, _ := args["city"].(string)
			return fmt.Sprintf("72°F and sunny in %s", city), nil
		},
	})

	callCount := 0
	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			callCount++
			if callCount == 1 {
				// Turn 1: request weather tool
				return "", "Thinking about weather...", []ToolCall{
					{
						ID:   "call_weather_1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "get_weather",
							Arguments: json.RawMessage(`{"city":"Austin"}`),
						},
					},
				}, nil
			}

			// Turn 2: observe result and respond with signat
			return "The weather in Austin is 72°F and sunny! 🦉☕", "Synthesizing answer", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)

	eventCh := make(chan HarnessEvent, 50)
	req := TurnRequest{
		SessionID: "test-session",
		Message:   "What's the weather in Austin?",
	}

	done := make(chan struct{})
	var events []HarnessEvent
	go func() {
		defer close(done)
		for ev := range eventCh {
			events = append(events, ev)
		}
	}()

	asstNode, err := harness.ExecuteTurn(context.Background(), req, eventCh)
	close(eventCh)
	<-done

	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if asstNode == nil {
		t.Fatalf("expected assistant node returned")
	}

	if !strings.Contains(asstNode.Content, "72°F and sunny") {
		t.Errorf("expected assistant content to contain weather, got: %q", asstNode.Content)
	}

	if asstNode.Metadata["signat"] != "🦉☕" {
		t.Errorf("expected signat '🦉☕', got: %q", asstNode.Metadata["signat"])
	}

	if len(asstNode.ToolCalls) != 1 || asstNode.ToolCalls[0].ID != "call_weather_1" {
		t.Errorf("expected 1 tool call 'call_weather_1', got: %v", asstNode.ToolCalls)
	}

	if len(asstNode.Observations) != 1 || !strings.Contains(asstNode.Observations[0].Result, "72°F") {
		t.Errorf("expected 1 observation containing 72°F, got: %v", asstNode.Observations)
	}

	// Verify session head was updated
	headID, err := storage.GetSessionHead("test-session")
	if err != nil || headID != asstNode.ID {
		t.Errorf("expected session head %s, got: %s (err: %v)", asstNode.ID, headID, err)
	}

	// Verify event sequence
	var kinds []HarnessEventKind
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}

	hasThought := false
	hasToolCall := false
	hasToolResult := false
	hasToken := false
	hasComplete := false

	for _, k := range kinds {
		switch k {
		case HarnessEventThought:
			hasThought = true
		case HarnessEventToolCall:
			hasToolCall = true
		case HarnessEventToolResult:
			hasToolResult = true
		case HarnessEventToken:
			hasToken = true
		case HarnessEventNodeComplete:
			hasComplete = true
		}
	}

	if !hasThought || !hasToolCall || !hasToolResult || !hasToken || !hasComplete {
		t.Errorf("missing expected events in stream: thought=%v, tool_call=%v, tool_result=%v, token=%v, complete=%v",
			hasThought, hasToolCall, hasToolResult, hasToken, hasComplete)
	}
}

func TestSessionHarness_DirectTurnWithoutTools(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			return "Hello there! 🦉☕", "Thinking...", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)

	req := TurnRequest{
		SessionID: "quick-chat",
		Message:   "Hi",
	}

	asstNode, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if asstNode.Content != "Hello there!" {
		t.Errorf("expected clean content 'Hello there!', got: %q", asstNode.Content)
	}
	if asstNode.Metadata["signat"] != "🦉☕" {
		t.Errorf("expected signat '🦉☕', got: %q", asstNode.Metadata["signat"])
	}
}

func TestSessionHarness_ContextCancellation(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			time.Sleep(50 * time.Millisecond)
			return "delayed response", "", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := harness.ExecuteTurn(ctx, TurnRequest{Message: "test"}, nil)
	if err == nil {
		t.Fatalf("expected error on cancelled context")
	}
}
