package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// TextViewLayer is a full-screen scrollable text viewer on the ViewStack (e.g. for /help, /config, /nodes).
type TextViewLayer struct {
	m        *Model
	name     string
	title    string
	content  string
	help     string
	viewport viewport.Model
	ready    bool
}

// NewTextViewLayer creates a new full-screen scrollable text layer.
func NewTextViewLayer(m *Model, name, title, content, helpText string) *TextViewLayer {
	if m != nil {
		m.ViewportOverride = content
		m.Viewport.SetContent(content)
		m.Viewport.GotoTop()
	}

	vp := viewport.New(m.Width, m.Height-4)
	if m.Height > 4 {
		vp.Height = m.Height - 4
	}
	if m.Width > 0 {
		vp.Width = m.Width
	}
	vp.SetContent(content)
	vp.GotoTop()

	if helpText == "" {
		helpText = "esc/q: return to chat • ↑/↓ or j/k to scroll • pgup/pgdn: page"
	}

	return &TextViewLayer{
		m:        m,
		name:     name,
		title:    title,
		content:  content,
		help:     helpText,
		viewport: vp,
		ready:    true,
	}
}

func (l *TextViewLayer) setModel(m *Model) {
	l.m = m
	if l.viewport.Width != m.Width || l.viewport.Height != m.Height-4 {
		l.viewport.Width = m.Width
		if m.Height > 4 {
			l.viewport.Height = m.Height - 4
		}
	}
}

func (l *TextViewLayer) Name() string {
	if l.name != "" {
		return l.name
	}
	return "text_view"
}

func (l *TextViewLayer) IsOverlay() bool {
	return false
}

func (l *TextViewLayer) View(width, height int) string {
	var s string
	if l.title != "" {
		s += titleStyle.Render(fmt.Sprintf(" PLEASE - %s ", l.title)) + "\n\n"
	}

	s += historyBoxStyle.Render(l.viewport.View())

	if l.m != nil {
		s += "\n\n" + l.m.renderFooterHelp(l.help)
	} else {
		s += "\n\n" + helpStyle.Render(l.help)
	}

	return s
}

func (l *TextViewLayer) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.viewport.Width = msg.Width
		if msg.Height > 4 {
			l.viewport.Height = msg.Height - 4
		}
		return nil, true

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			if l.m != nil {
				l.m.ViewportOverride = ""
				l.m.updateViewportContent()
				if l.m.ViewStack != nil && l.m.ViewStack.Top() == l {
					l.m.ViewStack.Pop()
					return nil, true
				}
			}
		case "j", "down":
			l.viewport.LineDown(1)
			return nil, true
		case "k", "up":
			l.viewport.LineUp(1)
			return nil, true
		case "g", "home":
			l.viewport.GotoTop()
			return nil, true
		case "G", "end":
			l.viewport.GotoBottom()
			return nil, true
		case "pgdown", "ctrl+d", "ctrl+f", "space":
			l.viewport.ViewDown()
			return nil, true
		case "pgup", "ctrl+u", "ctrl+b":
			l.viewport.ViewUp()
			return nil, true
		}

		var cmd tea.Cmd
		l.viewport, cmd = l.viewport.Update(msg)
		return cmd, true
	}

	return nil, false
}
