package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractSignat(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantClean  string
		wantSignat string
	}{
		{
			name:       "Trailing emojis with space",
			input:      "I have completed the refactoring! 🛠️💻",
			wantClean:  "I have completed the refactoring!",
			wantSignat: "🛠️💻",
		},
		{
			name:       "Trailing emoji on newline",
			input:      "Here is the proof:\n\n🧠📐",
			wantClean:  "Here is the proof:",
			wantSignat: "🧠📐",
		},
		{
			name:       "Explicit signat tags",
			input:      "Analysis complete. <signat>🔍📁</signat>",
			wantClean:  "Analysis complete.",
			wantSignat: "🔍📁",
		},
		{
			name:       "No signat present",
			input:      "Just a standard response without any emojis.",
			wantClean:  "Just a standard response without any emojis.",
			wantSignat: "",
		},
		{
			name:       "Single emoji",
			input:      "Done ✨",
			wantClean:  "Done",
			wantSignat: "✨",
		},
		{
			name:       "Persona system prompt",
			input:      "You are a grumpy librarian 📚☕",
			wantClean:  "You are a grumpy librarian",
			wantSignat: "📚☕",
		},
		{
			name:       "Trailing emoji followed by markdown prompt echo",
			input:      "The mathematical model is sound.\n\n🧠📐\n\n***Please proceed.***",
			wantClean:  "The mathematical model is sound.\n\n***Please proceed.***",
			wantSignat: "🧠📐",
		},
		{
			name:       "Trailing emoji with surrounding asterisks",
			input:      "Exploration complete.\n\n*🛠️💻*",
			wantClean:  "Exploration complete.",
			wantSignat: "🛠️💻",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotSignat := ExtractSignat(tt.input)
			if gotClean != tt.wantClean {
				t.Errorf("gotClean = %q, want %q", gotClean, tt.wantClean)
			}
			if gotSignat != tt.wantSignat {
				t.Errorf("gotSignat = %q, want %q", gotSignat, tt.wantSignat)
			}
		})
	}
}

func TestBuildLLMContext_SignatRetention(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	root, _ := mgr.CreateNode("", RoleSystem, "You are George 🦉📚", false)
	user, _ := mgr.CreateNode(root.ID, RoleUser, "Hello George!", false)
	asst, _ := mgr.CreateAssistantNode(user.ID, "Greetings! 🦉📜", "Thinking...", nil, false)

	// Mode 1: Default Clean Mode (SignatSteering = false)
	// Prompts sent to model must be 100% clean (no emojis injected into past turns)
	cleanMessages, err := mgr.BuildLLMContext(asst.ID, false)
	if err != nil {
		t.Fatalf("failed to build context: %v", err)
	}
	if cleanMessages[0].Content != "You are George" {
		t.Errorf("clean mode: expected root message to omit signat, got: %q", cleanMessages[0].Content)
	}
	if cleanMessages[2].Content != "Greetings!" {
		t.Errorf("clean mode: expected assistant message to omit signat, got: %q", cleanMessages[2].Content)
	}

	// But metadata in SQLite MUST still record the signat for TUI and Subway Map!
	storedAsst, _ := mgr.GetNode(asst.ID)
	if storedAsst.Metadata["signat"] != "🦉📜" {
		t.Errorf("expected stored assistant metadata to preserve signat '🦉📜', got: %q", storedAsst.Metadata["signat"])
	}

	// Mode 2: Opt-In Mode (SignatSteering = true)
	// Model gets layered genesis contract and sees emojis in context
	mgr.SignatSteering = true
	steeredMessages, err := mgr.BuildLLMContext(asst.ID, false)
	if err != nil {
		t.Fatalf("failed to build steered context: %v", err)
	}
	if !strings.Contains(steeredMessages[0].Content, "🦉📚") {
		t.Errorf("steered mode: expected root message to contain signat, got: %q", steeredMessages[0].Content)
	}
	if !strings.Contains(steeredMessages[0].Content, SignatSteeringContract) {
		t.Errorf("steered mode: expected root message to layer SignatSteeringContract, got: %q", steeredMessages[0].Content)
	}
	if steeredMessages[2].Content != "Greetings! 🦉📜" {
		t.Errorf("steered mode: expected assistant message to retain signat, got: %q", steeredMessages[2].Content)
	}
}

func TestSignatSteering_IntermediateToolCallNotPolluted(t *testing.T) {
	storage := &MockStorage{}
	graph := NewGraph()
	mgr := NewManager(graph, storage)
	mgr.SignatSteering = true

	root, _ := mgr.CreateNode("", RoleSystem, "You are George 🦉📚", false)
	user, _ := mgr.CreateNode(root.ID, RoleUser, "List files", false)

	tCall := ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}{
			Name:      "list_directory",
			Arguments: json.RawMessage(`{"path":"internal/tui/"}`),
		},
	}

	asst, err := mgr.CreateAssistantNode(user.ID, "", "Checking tui directory...", []ToolCall{tCall}, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}

	// Signat should be derived in metadata
	if asst.Metadata["signat"] == "" {
		t.Errorf("expected silent signat to be derived on node metadata")
	}

	// BuildLLMContext must NOT steer signat into the intermediate assistant message containing tool calls
	msgs, err := mgr.BuildLLMContext(asst.ID, false)
	if err != nil {
		t.Fatalf("failed to build context: %v", err)
	}

	for _, msg := range msgs {
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			if strings.Contains(msg.Content, "🔍") || strings.Contains(msg.Content, "📜") {
				t.Errorf("intermediate tool-calling assistant message should NOT contain signat, got: %q", msg.Content)
			}
			if msg.Content != "" {
				t.Errorf("expected empty content for tool-calling assistant message, got: %q", msg.Content)
			}
		}
	}
}
