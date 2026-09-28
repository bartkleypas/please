package tui

import tea "github.com/charmbracelet/bubbletea"

// chatLayer represents the permanent conversational root layer of the Please TUI.
type chatLayer struct {
	m *Model
}

func newChatLayer(m *Model) *chatLayer {
	return &chatLayer{m: m}
}

func (l *chatLayer) setModel(m *Model) {
	l.m = m
}

func (l *chatLayer) Name() string {
	return "chat"
}

func (l *chatLayer) IsOverlay() bool {
	return false
}

func (l *chatLayer) View(width, height int) string {
	if l.m == nil {
		return ""
	}
	return l.m.View()
}

func (l *chatLayer) Update(msg tea.Msg) (tea.Cmd, bool) {
	if l.m == nil {
		return nil, false
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	if newM, pCmd, handled := l.m.handlePacingKeys(keyMsg); handled {
		l.m = newM
		return pCmd, true
	}

	var cmd tea.Cmd
	var handled bool
	l.m, cmd, handled = l.m.handleChatKeys(keyMsg)
	if handled {
		return cmd, true
	}

	switch keyMsg.String() {
	case "enter":
		newM, eCmd := l.m.handleEnterKey()
		if nm, ok := newM.(*Model); ok {
			l.m = nm
		}
		return eCmd, true
	}

	var tiCmd tea.Cmd
	l.m.TextInput, tiCmd = l.m.TextInput.Update(keyMsg)
	return tea.Batch(cmd, tiCmd), true
}
