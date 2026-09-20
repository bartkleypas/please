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

func TestSessionHarness_RunwayWrapUp(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name: "infinite_tool",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "infinite output", nil
		},
	})

	callStep := 0
	sawToolsOnFinalStep := false
	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			callStep++
			if callStep < 3 {
				// Steps 1 & 2: request infinite_tool
				return "", "", []ToolCall{
					{
						ID:   fmt.Sprintf("call_%d", callStep),
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "infinite_tool",
							Arguments: json.RawMessage(fmt.Sprintf(`{"step":%d}`, callStep)),
						},
					},
				}, nil
			}

			// Step 3 (maxDepth - 1): tools should be suppressed by runway wrap-up
			if len(tools) > 0 {
				sawToolsOnFinalStep = true
			}
			return "Synthesizing final summary after reaching runway limit", "", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)

	req := TurnRequest{
		SessionID:    "runway-test",
		Message:      "Execute until runway ends",
		MaxToolDepth: 3, // Set runway to 3 steps
	}

	asstNode, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if sawToolsOnFinalStep {
		t.Errorf("expected tools to be suppressed on final step (depth == maxDepth - 1)")
	}

	if !strings.Contains(asstNode.Content, "Synthesizing final summary") {
		t.Errorf("expected assistant to conclude with summary, got: %q", asstNode.Content)
	}
}

func TestSessionHarness_LoopCircuitBreaker(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name: "ping_tool",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "pong", nil
		},
	})

	step := 0
	circuitBreakerTriggered := false
	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			step++
			// Inspect observations to see if circuit breaker triggered
			for _, m := range messages {
				if m.Role == RoleTool && strings.Contains(m.Content, "loop circuit breaker triggered") {
					circuitBreakerTriggered = true
					return "Loop detected, halting and summarizing.", "", nil, nil
				}
			}

			// Model stubbornly keeps calling identical tool with identical args
			return "", "", []ToolCall{
				{
					ID:   fmt.Sprintf("ping_%d", step),
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "ping_tool",
						Arguments: json.RawMessage(`{"host":"localhost"}`),
					},
				},
			}, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)

	req := TurnRequest{
		SessionID:    "loop-test",
		Message:      "Start stubborn loop",
		MaxToolDepth: 10,
	}

	asstNode, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if !circuitBreakerTriggered {
		t.Errorf("expected loop circuit breaker to trigger on 3rd identical call")
	}

	if !strings.Contains(asstNode.Content, "Loop detected, halting and summarizing.") {
		t.Errorf("expected assistant to respond after circuit breaker, got: %q", asstNode.Content)
	}
}

func TestSessionHarness_PermissionGate_Allowed(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	toolExecuted := false
	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name:        "dangerous_exec",
		Category:    CategoryExecute,
		Interactive: true,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			toolExecuted = true
			return "Command executed successfully", nil
		},
	})

	callCount := 0
	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			callCount++
			if callCount == 1 {
				return "", "Preparing exec", []ToolCall{
					{
						ID:   "call_exec_1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "dangerous_exec",
							Arguments: json.RawMessage(`{"cmd":"rm -rf /tmp/test"}`),
						},
					},
				}, nil
			}
			return "Done executing", "", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)
	gateCalled := false
	harness.PermissionGate = func(ctx context.Context, sessionID string, call ToolCall) (bool, error) {
		gateCalled = true
		if call.Function.Name != "dangerous_exec" {
			t.Errorf("unexpected tool in gate: %s", call.Function.Name)
		}
		return true, nil
	}

	req := TurnRequest{
		SessionID: "gate-allow-test",
		Message:   "Run dangerous tool",
	}

	asstNode, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if !gateCalled {
		t.Errorf("expected PermissionGate to be called")
	}
	if !toolExecuted {
		t.Errorf("expected tool to execute after gate approval")
	}
	if asstNode.Content != "Done executing" {
		t.Errorf("unexpected content: %q", asstNode.Content)
	}
}

func TestSessionHarness_PermissionGate_Denied(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	toolExecuted := false
	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name:        "dangerous_exec",
		Category:    CategoryExecute,
		Interactive: true,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			toolExecuted = true
			return "Should not run", nil
		},
	})

	callCount := 0
	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			callCount++
			if callCount == 1 {
				return "", "Preparing exec", []ToolCall{
					{
						ID:   "call_exec_1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "dangerous_exec",
							Arguments: json.RawMessage(`{"cmd":"rm -rf /tmp/test"}`),
						},
					},
				}, nil
			}
			// In turn 2, check that the model receives the denial observation
			lastMsg := messages[len(messages)-1]
			return fmt.Sprintf("Observed: %s", lastMsg.Content), "", nil, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)
	gateCalled := false
	harness.PermissionGate = func(ctx context.Context, sessionID string, call ToolCall) (bool, error) {
		gateCalled = true
		return false, nil // Operator explicitly denies consent
	}

	req := TurnRequest{
		SessionID: "gate-deny-test",
		Message:   "Run dangerous tool",
	}

	asstNode, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if !gateCalled {
		t.Errorf("expected PermissionGate to be called")
	}
	if toolExecuted {
		t.Errorf("tool executed despite PermissionGate denial!")
	}
	if !strings.Contains(asstNode.Content, "User denied execution of tool: dangerous_exec") {
		t.Errorf("expected denial message in observation, got: %q", asstNode.Content)
	}
}

func TestSessionHarness_PermissionGate_Error(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)

	mgr.Registry = NewToolRegistry()
	mgr.Registry.Register(Tool{
		Name:        "dangerous_exec",
		Category:    CategoryExecute,
		Interactive: true,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "ok", nil
		},
	})

	mockProvider := &MockLLMProvider{
		StreamHandler: func(messages []Message, tools []Tool) (string, string, []ToolCall, error) {
			return "", "", []ToolCall{
				{
					ID:   "call_exec_1",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "dangerous_exec",
						Arguments: json.RawMessage(`{}`),
					},
				},
			}, nil
		},
	}

	cfg := NewDefaultConfig()
	harness := NewSessionHarness(mgr, mockProvider, cfg)
	harness.PermissionGate = func(ctx context.Context, sessionID string, call ToolCall) (bool, error) {
		return false, context.Canceled
	}

	req := TurnRequest{
		SessionID: "gate-err-test",
		Message:   "Run tool",
	}

	_, err := harness.ExecuteTurn(context.Background(), req, nil)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}
