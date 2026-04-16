package tui

import (
	"fmt"
	"strings"

	"org.kleypas.please/internal/engine"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeMap
)

type Model struct {
	Manager   *engine.Manager
	Provider  engine.LLMProvider
	CurrentID string

	TextInput        textinput.Model
	IsThinking       bool
	Ready            bool
	Width            int
	Notification     string
	ViewMode         ViewMode
	Viewport         viewport.Model
	SpinnerFrame     int
	ViewportOverride string
	SetupMode        bool
	PersonaSetupMode bool
	// ChatHistoryBuffer stores the rendered chat history to allow incremental updates
	ChatHistoryBuffer string
}

func NewModel(g *engine.Graph, s engine.Storage, p engine.LLMProvider, currentID string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message or /command..."
	ti.Focus()

	setupMode := true
	if _, err := g.GetSystemRoot(); err == nil {
		setupMode = false
	}

	m := Model{
		Manager:   engine.NewManager(g, s),
		Provider:  p,
		CurrentID: currentID,
		TextInput: ti,
		Ready:     true,
		Width:     80, // Default fallback width
		Viewport:  viewport.New(80, 80),
		SetupMode: setupMode,
	}

	// Initialize the buffer with the current path content if not in setup mode
	if !setupMode {
		path, err := g.GetPath(currentID)
		if err == nil && len(path) > 0 {
			var s_buf strings.Builder
			wrapWidth := 80 - 4 // Default fallback width minus borders/padding
			for _, node := range path {
				roleStr := string(node.Role)
				wrappedContent := wrapText(node.Content, wrapWidth)
				s_buf.WriteString(fmt.Sprintf("%s:\n%s\n", roleStr, wrappedContent))
			}
			m.ChatHistoryBuffer = s_buf.String()
		}
	}

	return m
}
