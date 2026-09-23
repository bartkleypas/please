package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
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

func TestTextViewLayer_RenderContainsContent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")
	store, _ := storage.NewSQLiteStorage(dbPath, "")
	g := graph.NewGraph()
	mockProvider := &providers.MockLLMProvider{}
	m := NewModel(nil, g, store, mockProvider, "")
	m, _ = updateModel(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.Height != 40 {
		t.Fatalf("expected m.Height to be 40 after WindowSizeMsg, got %d", m.Height)
	}

	content := "--- 🦉 Please Help ---\nInteractive Commands:\n  /help Show this help message"
	layer := NewTextViewLayer(&m, "help", "Help", content, "")

	rendered := layer.View(100, 40)
	if !strings.Contains(rendered, "Please Help") {
		t.Fatalf("expected rendered view to contain 'Please Help', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Interactive Commands") {
		t.Fatalf("expected rendered view to contain 'Interactive Commands', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "PLEASE - Help") {
		t.Fatalf("expected rendered view to contain title header, got:\n%s", rendered)
	}

	// Exit setup mode and execute /help via TextInput
	m.TextInput.SetValue("sys")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	m.TextInput.SetValue("/help")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	fullView := m.View()
	if !strings.Contains(fullView, "Please Help") {
		t.Fatalf("expected m.View() to render help content, got:\n%s", fullView)
	}
	if !strings.Contains(fullView, "Interactive Commands") {
		t.Fatalf("expected m.View() to render interactive commands, got:\n%s", fullView)
	}
}
