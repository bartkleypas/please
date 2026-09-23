package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTextViewLayer_LifecycleAndNavigation(t *testing.T) {
	root := &mockLayer{name: "root"}
	stack := NewViewStack(root)

	m := &Model{
		Width:     80,
		Height:    24,
		ViewStack: stack,
	}

	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	layer := NewTextViewLayer(m, "help", "Help", content, "")
	stack.Push(layer)

	if stack.Len() != 2 || stack.Top().Name() != "help" {
		t.Fatalf("expected top to be help at depth 2, got %q (len %d)", stack.Top().Name(), stack.Len())
	}
	if layer.IsOverlay() {
		t.Errorf("expected TextViewLayer to not be an overlay")
	}

	view := layer.View(80, 24)
	if len(view) == 0 {
		t.Errorf("expected non-empty view")
	}

	// 1. Navigation keys are consumed
	_, handled := stack.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !handled {
		t.Errorf("expected down arrow to be handled")
	}

	_, handled = stack.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if !handled {
		t.Errorf("expected 'j' to be handled")
	}

	// 2. 'esc' pops back to root
	_, handled = stack.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !handled {
		t.Errorf("expected esc to be handled")
	}
	if stack.Len() != 1 || stack.Top().Name() != "root" {
		t.Fatalf("expected top to return to root after esc, got %q (len %d)", stack.Top().Name(), stack.Len())
	}
}

func TestTextViewLayer_QuitKey(t *testing.T) {
	root := &mockLayer{name: "root"}
	stack := NewViewStack(root)

	m := &Model{
		Width:     80,
		Height:    24,
		ViewStack: stack,
	}

	layer := NewTextViewLayer(m, "config", "Config", "sample", "")
	stack.Push(layer)

	// 'q' pops the layer
	_, handled := stack.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !handled {
		t.Errorf("expected 'q' to be handled")
	}
	if stack.Len() != 1 {
		t.Errorf("expected 'q' to pop layer, got len %d", stack.Len())
	}
}
