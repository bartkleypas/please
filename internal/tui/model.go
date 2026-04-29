package tui

import (
	"time"

	"org.kleypas.please/internal/engine"
	"org.kleypas.please/internal/server"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
)

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeMap
)

type Model struct {
	Config    *engine.Config
	Manager   *engine.Manager
	Provider  engine.LLMProvider
	Server    *server.Server
	CurrentID string

	TextInput        textarea.Model
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
	StreamToolCallChan      <-chan []engine.ToolCall
	StreamErrChan           <-chan error

	// Interactive Map state
	MapNodeIDs        []string
	MapSelectionIndex int

	// Tool handling fields
	PendingToolCalls         []engine.ToolCall
	AwaitingToolConfirmation bool

	// Animation state
	LastActivity time.Time
}

func NewModel(cfg *engine.Config, g *engine.Graph, s engine.Storage, p engine.LLMProvider, currentID string) Model {
	ti := textarea.New()
	ti.Placeholder = "Type a message or /command..."
	ti.Focus()
	ti.ShowLineNumbers = false

	setupMode := true
	if _, err := g.GetSystemRoot(); err == nil {
		setupMode = false
	}

	mgr := engine.NewManager(g, s)
	mgr.RegisterDefaultTools()

	m := Model{
		Config:    cfg,
		Manager:   mgr,
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
