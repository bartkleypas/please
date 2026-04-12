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
	Graph     *engine.Graph
	Storage   engine.Storage
	Provider  engine.LLMProvider
	CurrentID string

	TextInput    textinput.Model
	IsThinking   bool
	Ready        bool
	Width        int
	Notification string
	ViewMode     ViewMode
	Viewport         viewport.Model
	SpinnerFrame     int
	ViewportOverride string
	SetupMode        bool
	PersonaSetupMode bool
}

func NewModel(g *engine.Graph, s engine.Storage, p engine.LLMProvider, currentID string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message or /command..."
	ti.Focus()

	setupMode := true
	if _, err := g.GetSystemRoot(); err == nil {
		setupMode = false
	}

	return Model{
		Graph:     g,
		Storage:   s,
		Provider:  p,
		CurrentID: currentID,
		TextInput: ti,
		Ready:     true,
		Width:     80, // Default fallback width
		Viewport:  viewport.New(80, 80),
		SetupMode: setupMode,
	}
}
