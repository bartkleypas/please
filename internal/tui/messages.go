package tui

import (
	"org.kleypas.please/internal/engine"
)

// tickMsg is sent to trigger the spinner animation
type tickMsg struct{}

// llmStreamMsg is sent for each chunk of content in a streaming response
type llmStreamMsg struct {
	content  string
	parentID string
}

// llmThoughtStreamMsg is sent for each chunk of reasoning in a streaming response
type llmThoughtStreamMsg struct {
	thought  string
	parentID string
}

// llmStreamFinishedMsg is sent when a streaming response completes
type llmStreamFinishedMsg struct {
	err       error
	parentID  string
	thought   string
	toolCalls []engine.ToolCall
}

// streamResponseMsg is sent to initialize the streaming channels in the model
type streamResponseMsg struct {
	contentChan  <-chan string
	thoughtChan  <-chan string
	toolCallChan <-chan []engine.ToolCall
	errChan      <-chan error
	parentID     string
}

// llmResponseMsg is sent for non-streaming LLM responses
type llmResponseMsg struct {
	message  *engine.Message
	err      error
	parentID string
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
