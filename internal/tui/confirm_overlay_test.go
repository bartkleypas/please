package tui

import (
	"testing"

	"github.com/bartkleypas/please/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)


func TestConfirmOverlay_Prune(t *testing.T) {
	root := &mockLayer{name: "root"}
	stack := NewViewStack(root)

	confirmed := false
	cancelled := false

	overlay := NewPruneConfirmOverlay(stack, "node-123", func() tea.Cmd {
		confirmed = true
		return nil
	}, func() tea.Cmd {
		cancelled = true
		return nil
	})

	stack.Push(overlay)
	if stack.Len() != 2 {
		t.Fatalf("expected stack len 2, got %d", stack.Len())
	}
	if !overlay.IsOverlay() {
		t.Errorf("expected overlay to return IsOverlay=true")
	}

	// 1. Irrelevant keys should be absorbed and NOT leak to root
	_, handled := stack.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if !handled {
		t.Errorf("expected 'j' to be absorbed by overlay")
	}
	if confirmed || cancelled {
		t.Errorf("expected neither confirmed nor cancelled on 'j'")
	}
	if stack.Len() != 2 {
		t.Errorf("expected overlay to remain on stack on 'j'")
	}
	if root.lastMsg != nil {
		t.Errorf("root received leaked message: %v", root.lastMsg)
	}

	// 2. 'y' confirms and pops overlay
	_, handled = stack.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !handled {
		t.Errorf("expected 'y' to be handled")
	}
	if !confirmed {
		t.Errorf("expected confirmed=true on 'y'")
	}
	if stack.Len() != 1 {
		t.Errorf("expected overlay to pop off stack after confirm, got len %d", stack.Len())
	}
}

func TestConfirmOverlay_Compact(t *testing.T) {
	root := &mockLayer{name: "root"}
	stack := NewViewStack(root)

	cancelled := false
	overlay := NewCompactConfirmOverlay(stack, 5, nil, func() tea.Cmd {
		cancelled = true
		return nil
	})
	stack.Push(overlay)

	// 'n' cancels and pops overlay
	_, handled := stack.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !handled {
		t.Errorf("expected 'n' to be handled")
	}
	if !cancelled {
		t.Errorf("expected cancelled=true on 'n'")
	}
	if stack.Len() != 1 {
		t.Errorf("expected stack len 1 after cancel, got %d", stack.Len())
	}
}

func TestConfirmOverlay_ToolRedirectionWarning(t *testing.T) {
	root := &mockLayer{name: "root"}
	stack := NewViewStack(root)

	tc := domain.ToolCall{}
	tc.Function.Name = "execute_bash"
	tc.Function.Arguments = []byte(`{"command":"cat file | grep secret > /tmp/out"}`)
	tools := []domain.ToolCall{tc}

	overlay := NewToolConfirmOverlay(stack, tools, nil, nil)
	view := overlay.View(80, 24)
	if overlay.warning == "" {
		t.Errorf("expected redirection warning to be detected")
	}
	if overlay.Name() != "confirm_tool" {
		t.Errorf("expected name 'confirm_tool', got %q", overlay.Name())
	}
	if len(view) == 0 {
		t.Errorf("expected non-empty view")
	}
}
