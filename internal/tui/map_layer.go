package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// MapLayer represents the full-screen interactive DAG trajectory visualizer on the ViewStack.
type MapLayer struct {
	m *Model
}

func newMapLayer(m *Model) *MapLayer {
	return &MapLayer{m: m}
}

func (l *MapLayer) setModel(m *Model) {
	l.m = m
}

func (l *MapLayer) Name() string {
	return "map"
}

func (l *MapLayer) IsOverlay() bool {
	return false
}

func (l *MapLayer) View(width, height int) string {
	if l.m == nil {
		return ""
	}
	return l.m.View()
}

func (l *MapLayer) Update(msg tea.Msg) (tea.Cmd, bool) {
	if l.m == nil {
		return nil, false
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	newM, cmd := l.m.handleMapKeys(keyMsg)
	l.m = newM

	// If handleMapKeys transitioned back to ModeChat (e.g. on esc or enter jump), pop from stack
	if l.m.ViewMode == ModeChat && l.m.ViewStack != nil && l.m.ViewStack.Top() == l {
		l.m.ViewStack.Pop()
	}

	return cmd, true
}
