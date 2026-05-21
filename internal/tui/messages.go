package tui

import (
	"github.com/bartkleypas/please/internal/engine"
)

// tickMsg is sent to trigger the spinner animation
type tickMsg struct{}

// pacingTickMsg is sent to type out characters at a natural reading pace
type pacingTickMsg struct{}

// llmStreamMsg is sent for each chunk of content in a streaming response
type llmStreamMsg struct {
	content      string
	parentID     string
	activeNodeID string // If set, we are updating an existing node
}

// llmThoughtStreamMsg is sent for each chunk of reasoning in a streaming response
type llmThoughtStreamMsg struct {
	thought      string
	parentID     string
	activeNodeID string // If set, we are updating an existing node
}

// llmStreamFinishedMsg is sent when a streaming response completes
type llmStreamFinishedMsg struct {
	err          error
	parentID     string
	activeNodeID string // If set, this was a resumption
	thought      string
	toolCalls    []engine.ToolCall
}

// streamResponseMsg is sent to initialize the streaming channels in the model
type streamResponseMsg struct {
	contentChan  <-chan string
	thoughtChan  <-chan string
	toolCallChan <-chan []engine.ToolCall
	errChan      <-chan error
	parentID     string
	activeNodeID string // If set, this is an interleaving resumption
}

// exportResultMsg is sent when the export file operation completes
type exportResultMsg struct {
	filename string
	err      error
}

// syncResultMsg is sent after a manual synchronization with the vault
type syncResultMsg struct {
	err error
}

type compactionFinishedMsg struct {
	node *engine.Node
	err  error
}

// toolsExecutedMsg is sent when tool calls have been executed and saved to the DAG,
// but before the resumption stream has started.
type toolsExecutedMsg struct {
	lastNodeID   string
	activeNodeID string // The node being interleaved
	err          error
}
