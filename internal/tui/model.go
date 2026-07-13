package tui

import (
	"time"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/server"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
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
	// CurrentStreamingThought stores the reasoning currently being streamed
	CurrentStreamingThought string
	StreamContentChan       <-chan string
	StreamThoughtChan       <-chan string
	StreamToolCallChan      <-chan []engine.ToolCall
	StreamErrChan           <-chan error
	StreamCancel            func() // Cancellation function for the active LLM stream
	InterleavingNodeID      string // The Assistant node currently receiving observations

	// Interactive Map state
	MapNodeIDs        []string
	MapSelectionIndex int
	SearchInput       textinput.Model
	Searching         bool
	SearchQuery       string
	CollapsedNodes    map[string]bool

	// Deletion state
	AwaitingPruneConfirmation bool
	PruneTargetID             string

	// Compaction state
	AwaitingCompactConfirmation bool
	CompactTargetIDs            []string
	IsCompressing               bool

	// Tool handling fields
	PendingToolCalls         []engine.ToolCall
	AwaitingToolConfirmation bool

	// Animation state
	LastActivity time.Time

	// Audit mode state
	AuditMode bool

	// Pacing state
	PacingBuffer       []rune
	PacingActive       bool
	PacingSkipped      bool
	LLMFinished        bool
	FinishedMsgPending *llmStreamFinishedMsg

	// Pending attached images
	PendingImages []string
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

	si := textinput.New()
	si.Placeholder = "Fuzzy search content..."
	si.Prompt = " / "

	m := Model{
		Config:         cfg,
		Manager:        mgr,
		Provider:       p,
		CurrentID:      currentID,
		TextInput:      ti,
		SearchInput:    si,
		Ready:          true,
		Width:          0, // Initialize to 0; wait for WindowSizeMsg
		Viewport:       viewport.New(80, 80),
		SetupMode:      setupMode,
		CollapsedNodes: make(map[string]bool),
		AuditMode:      false,
	}

	return m
}
