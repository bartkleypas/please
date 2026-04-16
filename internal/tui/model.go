package tui

import (
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
	// CurrentStreamingContent stores the content of the message currently being streamed
	CurrentStreamingContent string
	StreamContentChan       <-chan string
	StreamErrChan           <-chan error
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
		Width:     0, // Initialize to 0; wait for WindowSizeMsg
		Viewport:  viewport.New(80, 80),
		SetupMode: setupMode,
	}

	return m
}
