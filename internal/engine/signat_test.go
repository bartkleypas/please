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
