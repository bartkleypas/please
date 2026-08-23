package engine

import (
	"testing"
)

func TestExtractSignat(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantClean    string
		wantSignat   string
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

	messages, err := mgr.BuildLLMContext(asst.ID, false)
	if err != nil {
		t.Fatalf("failed to build context: %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Verify root system message retains signat
	if messages[0].Content != "You are George 🦉📚" {
		t.Errorf("expected root message to retain signat, got: %q", messages[0].Content)
	}

	// Verify assistant message retains signat
	if messages[2].Content != "Greetings! 🦉📜" {
		t.Errorf("expected assistant message to retain signat, got: %q", messages[2].Content)
	}
}
