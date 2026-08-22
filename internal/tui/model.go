package tui

import (
	"time"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/server"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeMap
)

type Model struct {
	Config   *engine.Config
	Manager  *engine.Manager
	Provider engine.LLMProvider
	Server   *server.Server // Host engine server daemon in standalone mode

	Ready             bool
	Width             int
	Height            int
	ViewMode          ViewMode
	CurrentID         string
	PendingUserNodeID string
	TextInput         textarea.Model
	Viewport          viewport.Model
	Notification      string
	ViewportOverride  string // Temporary full-screen text display (e.g. /help, /config)
	SetupMode         bool   // Flag indicating initial persona creation prompt
	PersonaSetupMode  bool   // Flag indicating persona switching prompt
	SpinnerFrame      int
	IsThinking        bool
	RemoteURL         string // Active remote daemon URL if in connected client mode

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
	if cfg == nil {
		cfg = engine.NewDefaultConfig()
	} else {
		if cfg.Server == nil {
			defaultCfg := engine.NewDefaultConfig()
			cfg.Server = defaultCfg.Server
		}
		if cfg.Client == nil {
			defaultCfg := engine.NewDefaultConfig()
			cfg.Client = defaultCfg.Client
		}
	}

	ti := textarea.New()
	ti.Placeholder = "Type a message or /command..."
	ti.Focus()
	ti.ShowLineNumbers = false
	ti.Prompt = "▌ "
	ti.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	ti.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#2d5a45"))

	setupMode := true
	if _, err := g.GetSystemRoot(); err == nil {
		setupMode = false
	}

	mgr := engine.NewManager(g, s)
	mgr.RegisterDefaultTools(cfg.GetWorkspaceDir())

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

// ContextStats calculates the estimated active context size and returns the token count, limit, percentage, and theme color
func (m Model) ContextStats() (int, int, int, lipgloss.Color) {
	limit := 32768
	if m.Config != nil && m.Config.Server != nil && m.Config.Server.Options != nil && m.Config.Server.Options.NumCtx != nil {
		limit = *m.Config.Server.Options.NumCtx
	}

	var charCount int
	if m.Manager != nil {
		if path, err := m.Manager.GetPath(m.CurrentID); err == nil {
			for _, node := range path {
				charCount += len(node.Content) + len(node.Thought)
				for _, obs := range node.Observations {
					charCount += len(obs.Result)
				}
			}
		}
	}
	charCount += len(m.TextInput.Value())

	// Heuristic: ~3.8 characters per token for mixed English & code
	estimatedTokens := int(float64(charCount) / 3.8)
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}

	pct := (estimatedTokens * 100) / limit
	if pct > 100 {
		pct = 100
	}

	var color lipgloss.Color
	if pct < 60 {
		color = lipgloss.Color("#4ade80") // emerald userStyle
	} else if pct < 85 {
		color = lipgloss.Color("#facc15") // warm amber markStyle
	} else {
		color = lipgloss.Color("#fb7185") // coral warningStyle
	}

	return estimatedTokens, limit, pct, color
}
