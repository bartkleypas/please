package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type mockLayer struct {
	name      string
	overlay   bool
	handled   bool
	cmd       tea.Cmd
	lastMsg   tea.Msg
	viewText  string
	escHandle bool
}

func (m *mockLayer) Name() string                  { return m.name }
func (m *mockLayer) IsOverlay() bool               { return m.overlay }
func (m *mockLayer) View(width, height int) string { return m.viewText }
func (m *mockLayer) Update(msg tea.Msg) (tea.Cmd, bool) {
	m.lastMsg = msg
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "esc" {
		return m.cmd, m.escHandle
	}
	return m.cmd, m.handled
}

func TestViewStack_PushPopInvariants(t *testing.T) {
	root := &mockLayer{name: "root", overlay: false}
	stack := NewViewStack(root)

	if stack.Len() != 1 {
		t.Fatalf("expected len 1, got %d", stack.Len())
	}
	if stack.Top() != root {
		t.Fatalf("expected top to be root, got %v", stack.Top())
	}

	// Invariant: root cannot be popped
	popped := stack.Pop()
	if popped != nil {
		t.Errorf("expected popping root to return nil, got %v", popped)
	}
	if stack.Len() != 1 {
		t.Errorf("expected len to remain 1, got %d", stack.Len())
	}

	// Push layer 1
	layer1 := &mockLayer{name: "layer1", overlay: false}
	stack.Push(layer1)
	if stack.Len() != 2 {
		t.Fatalf("expected len 2, got %d", stack.Len())
	}
	if stack.Top() != layer1 {
		t.Fatalf("expected top to be layer1, got %v", stack.Top())
	}

	// Push layer 2 (overlay)
	layer2 := &mockLayer{name: "layer2", overlay: true}
	stack.Push(layer2)
	if stack.Len() != 3 {
		t.Fatalf("expected len 3, got %d", stack.Len())
	}
	if stack.Top() != layer2 {
		t.Fatalf("expected top to be layer2, got %v", stack.Top())
	}

	// Check LayerBelow
	below2 := stack.LayerBelow(layer2)
	if below2 != layer1 {
		t.Errorf("expected layer below layer2 to be layer1, got %v", below2)
	}
	below1 := stack.LayerBelow(layer1)
	if below1 != root {
		t.Errorf("expected layer below layer1 to be root, got %v", below1)
	}
	belowRoot := stack.LayerBelow(root)
	if belowRoot != nil {
		t.Errorf("expected layer below root to be nil, got %v", belowRoot)
	}

	// Pop layer 2
	popped2 := stack.Pop()
	if popped2 != layer2 {
		t.Errorf("expected popped layer to be layer2, got %v", popped2)
	}
	if stack.Top() != layer1 {
		t.Errorf("expected top to be layer1, got %v", stack.Top())
	}
	if stack.Len() != 2 {
		t.Errorf("expected len 2, got %d", stack.Len())
	}
}

func TestViewStack_UpdateDispatchAndExclusiveFocus(t *testing.T) {
	root := &mockLayer{name: "root", overlay: false}
	layer1 := &mockLayer{name: "layer1", overlay: true, handled: true}
	stack := NewViewStack(root)
	stack.Push(layer1)

	// Send message: top layer should receive it exclusively
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	_, handled := stack.Update(msg)
	if !handled {
		t.Errorf("expected message to be handled by top layer")
	}
	if k, ok := layer1.lastMsg.(tea.KeyMsg); !ok || k.String() != msg.String() {
		t.Errorf("expected layer1 to receive msg %v, got %v", msg, layer1.lastMsg)
	}
	if root.lastMsg != nil {
		t.Errorf("expected root not to receive msg when layer1 is on top! Root received: %v", root.lastMsg)
	}
}

func TestViewStack_UniversalEscContract(t *testing.T) {
	root := &mockLayer{name: "root", overlay: false}
	layer1 := &mockLayer{name: "layer1", overlay: true, escHandle: false}
	stack := NewViewStack(root)
	stack.Push(layer1)

	// Layer 1 does not consume esc -> ViewStack automatically pops it
	escMsg := tea.KeyMsg{Type: tea.KeyEscape}
	_, handled := stack.Update(escMsg)
	if !handled {
		t.Errorf("expected esc to be handled by automatic stack pop")
	}
	if stack.Len() != 1 {
		t.Errorf("expected stack len 1 after esc pop, got %d", stack.Len())
	}
	if stack.Top() != root {
		t.Errorf("expected top to be root after esc pop, got %v", stack.Top())
	}

	// Now on root, unhandled esc does NOT pop root
	_, handledRoot := stack.Update(escMsg)
	if handledRoot {
		t.Errorf("expected unhandled esc on root to return handled=false")
	}
	if stack.Len() != 1 {
		t.Errorf("expected root not to be popped, got len %d", stack.Len())
	}

	// If layer explicitly consumes esc, it should NOT pop
	layer2 := &mockLayer{name: "layer2", overlay: true, escHandle: true}
	stack.Push(layer2)
	_, handledLayer2 := stack.Update(escMsg)
	if !handledLayer2 {
		t.Errorf("expected layer2 to handle esc")
	}
	if stack.Len() != 2 {
		t.Errorf("expected layer2 to remain on stack when consuming esc, got len %d", stack.Len())
	}
}
