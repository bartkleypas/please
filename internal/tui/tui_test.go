package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bartkleypas/please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdateStateTransitions(t *testing.T) {
	// 1. Setup dependencies
	tmpDir, err := os.MkdirTemp("", "please-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.jsonl")

	storage := engine.NewJSONLStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Hello world",
	}

	// 2. Initialize Model
	pacing := false
	cfg := &engine.Config{NaturalPacing: &pacing}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// Ensure we start in SetupMode if graph is empty
	if !m.SetupMode {
		t.Error("Expected model to start in SetupMode for empty graph")
	}

	// 3. Simulate System Prompt input
	m.TextInput.SetValue("You are a helpful assistant")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.SetupMode {
		t.Error("Expected model to exit SetupMode after system prompt")
	}
	if m.CurrentID == "" {
		t.Error("Expected CurrentID to be set after system prompt")
	}

	// 4. Simulate User Message input
	m.TextInput.SetValue("Hi there")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.IsThinking {
		t.Error("Expected model to be in IsThinking state after user input")
	}

	// 5. Simulate LLM Stream Message
	m, _ = updateModel(m, llmStreamMsg{content: "Hello", parentID: m.CurrentID})

	if !m.IsThinking {
		t.Error("Expected IsThinking to be true during streaming")
	}
	if m.CurrentStreamingContent != "Hello" {
		t.Errorf("Expected CurrentStreamingContent to be 'Hello', got '%s'", m.CurrentStreamingContent)
	}

	// 6. Simulate LLM Stream Finished
	m, _ = updateModel(m, llmStreamFinishedMsg{parentID: m.CurrentID})

	if m.IsThinking {
		t.Error("Expected IsThinking to be false after stream finished")
	}
	if m.CurrentStreamingContent != "" {
		t.Error("Expected CurrentStreamingContent to be cleared after finish")
	}
}

func TestThoughtStreaming(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "please-test-thought")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	cfg := &engine.Config{NaturalPacing: &pacing}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Setup - Create root node
	m.TextInput.SetValue("System prompt")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	// 2. User Message
	m.TextInput.SetValue("Hello")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	userID := m.CurrentID

	// 3. Thought stream
	m, _ = updateModel(m, llmThoughtStreamMsg{thought: "Thinking...", parentID: userID})
	if m.CurrentStreamingThought != "Thinking..." {
		t.Errorf("Expected streaming thought 'Thinking...', got '%s'", m.CurrentStreamingThought)
	}

	// 4. Content stream
	m, _ = updateModel(m, llmStreamMsg{content: "Hi!", parentID: userID})
	if m.CurrentStreamingContent != "Hi!" {
		t.Errorf("Expected streaming content 'Hi!', got '%s'", m.CurrentStreamingContent)
	}

	// 5. Finish stream
	m, _ = updateModel(m, llmStreamFinishedMsg{parentID: userID})

	// 6. Verify persistence in the Assistant Node
	// The path should now be: User -> Assistant (containing Thought)
	finalNode, _ := m.Manager.GetNode(m.CurrentID)

	if finalNode.Thought != "Thinking..." {
		t.Errorf("Expected saved thought 'Thinking...', got '%s'", finalNode.Thought)
	}

	if finalNode.Content != "Hi!" {
		t.Errorf("Expected saved content 'Hi!', got '%s'", finalNode.Content)
	}
}

func TestHandleCommand(t *testing.T) {
	storage := engine.NewJSONLStorage(":memory:", "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	cfg := &engine.Config{NaturalPacing: &pacing}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	tests := []struct {
		input    string
		handled  bool
		expected string // Expected notification or state change hint
	}{
		{"hello", false, ""},
		{"/unknown", true, "Unknown command: /unknown"},
		{"/persona", true, ""}, // Persona setup mode triggered
		{"/audit", true, "Audit Mode enabled: Internal nodes visible."},
	}

	for _, tt := range tests {
		newM, _, handled := m.HandleCommand(tt.input)
		if handled != tt.handled {
			t.Errorf("input %s: expected handled %v, got %v", tt.input, tt.handled, handled)
		}

		resModel := newM.(*Model)
		if tt.handled && tt.expected != "" && resModel.Notification != tt.expected {
			t.Errorf("input %s: expected notification %s, got %s", tt.input, tt.expected, resModel.Notification)
		}

		if tt.input == "/persona" && !resModel.PersonaSetupMode {
			t.Errorf("input %s: expected PersonaSetupMode to be true", tt.input)
		}

		if tt.input == "/audit" && !resModel.AuditMode {
			t.Errorf("input %s: expected AuditMode to be true", tt.input)
		}
	}
}

// Helper to handle the (tea.Model, tea.Cmd) return from Update
func updateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	// Our Update returns (tea.Model, tea.Cmd), but we want the concrete Model
	newModel, cmd := m.Update(msg)
	return *newModel.(*Model), cmd
}

func TestNaturalPacing(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "please-test-pacing")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := true
	cfg := &engine.Config{NaturalPacing: &pacing}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Exit setup mode
	m.TextInput.SetValue("System prompt")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	// 2. User input
	m.TextInput.SetValue("Paced request")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	userID := m.CurrentID

	// 3. Stream paced chunk: "A. B"
	m, _ = updateModel(m, llmStreamMsg{content: "A. B", parentID: userID})

	if m.CurrentStreamingContent != "" {
		t.Errorf("Expected CurrentStreamingContent to be empty initially due to pacing, got '%s'", m.CurrentStreamingContent)
	}
	if !m.PacingActive {
		t.Error("Expected PacingActive to be true")
	}
	if string(m.PacingBuffer) != "A. B" {
		t.Errorf("Expected PacingBuffer to contain 'A. B', got '%s'", string(m.PacingBuffer))
	}

	// 4. Pop first character ('A')
	m, _ = updateModel(m, pacingTickMsg{})
	if m.CurrentStreamingContent != "A" {
		t.Errorf("Expected CurrentStreamingContent to be 'A', got '%s'", m.CurrentStreamingContent)
	}

	// 5. Pop second character ('.')
	m, _ = updateModel(m, pacingTickMsg{})
	if m.CurrentStreamingContent != "A." {
		t.Errorf("Expected CurrentStreamingContent to be 'A.', got '%s'", m.CurrentStreamingContent)
	}

	// 6. Test Skip pacing mid-stream
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.CurrentStreamingContent != "A. B" {
		t.Errorf("Expected CurrentStreamingContent to be fully flushed to 'A. B', got '%s'", m.CurrentStreamingContent)
	}
	if m.PacingActive {
		t.Error("Expected PacingActive to be false after skip")
	}
}

func TestToolExecutionErrorRetention(t *testing.T) {
	// 1. Setup dependencies
	tmpDir, err := os.MkdirTemp("", "please-test-tool-err")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, err := engine.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatal(err)
	}
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}

	pacing := false
	cfg := &engine.Config{NaturalPacing: &pacing}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// Register a mock tool that returns both output and error
	m.Manager.Registry.Register(engine.Tool{
		Name: "fail_tool",
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "some partial output here", fmt.Errorf("something went wrong")
		},
	})

	// Create a root system node and an assistant node
	sysNode, err := m.Manager.CreateNode("", engine.RoleSystem, "System prompt", false)
	if err != nil {
		t.Fatal(err)
	}
	
	assistantNode, err := m.Manager.CreateAssistantNode(sysNode.ID, "Running tool...", "", []engine.ToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      "fail_tool",
				Arguments: json.RawMessage(`{}`),
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	m.CurrentID = assistantNode.ID
	m.InterleavingNodeID = assistantNode.ID
	m.PendingToolCalls = assistantNode.ToolCalls

	// Trigger execution command
	cmd := m.executeToolsCmd()
	msg := cmd() // Run synchronously

	resMsg, ok := msg.(toolsExecutedMsg)
	if !ok {
		t.Fatalf("expected toolsExecutedMsg, got %T", msg)
	}
	if resMsg.err != nil {
		t.Fatalf("unexpected error inside toolsExecutedMsg: %v", resMsg.err)
	}

	// Verify observations in storage / memory
	node, err := m.Manager.GetNode(assistantNode.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(node.Observations) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(node.Observations))
	}

	obs := node.Observations[0]
	if obs.ToolCallID != "call_123" {
		t.Errorf("expected ToolCallID 'call_123', got '%s'", obs.ToolCallID)
	}

	expectedResult := "Error: something went wrong\nOutput:\nsome partial output here"
	if obs.Result != expectedResult {
		t.Errorf("expected observation result:\n%s\ngot:\n%s", expectedResult, obs.Result)
	}
}
