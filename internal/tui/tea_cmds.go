package tui

import (
	"context"
	"time"

	"org.kleypas.please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func generateResponse(provider engine.LLMProvider, messages []engine.Message, tools []engine.Tool, parentID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		msg, err := provider.GenerateResponse(ctx, messages, tools)
		return llmResponseMsg{
			message:  msg,
			err:      err,
			parentID: parentID,
		}
	}
}

func streamResponse(provider engine.LLMProvider, messages []engine.Message, tools []engine.Tool, parentID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		contentChan, toolCallChan, errChan := provider.GenerateResponseStream(ctx, messages, tools)
		return streamResponseMsg{
			contentChan:  contentChan,
			toolCallChan: toolCallChan,
			errChan:      errChan,
			parentID:     parentID,
		}
	}
}

func waitForStream(contentChan <-chan string, toolCallChan <-chan []engine.ToolCall, errChan <-chan error, parentID string) tea.Cmd {
	return func() tea.Msg {
		select {
		case content, ok := <-contentChan:
			if !ok {
				// Content closed, now wait for potential tool calls or error
				select {
				case tc := <-toolCallChan:
					return llmStreamFinishedMsg{parentID: parentID, toolCalls: tc}
				case err := <-errChan:
					return llmStreamFinishedMsg{parentID: parentID, err: err}
				default:
					return llmStreamFinishedMsg{parentID: parentID}
				}
			}
			return llmStreamMsg{content: content, parentID: parentID}
		case tc := <-toolCallChan:
			return llmStreamFinishedMsg{parentID: parentID, toolCalls: tc}
		case err := <-errChan:
			if err != nil {
				return llmStreamFinishedMsg{err: err, parentID: parentID}
			}
			return llmStreamFinishedMsg{parentID: parentID}
		}
	}
}

// tick is a command that sends a tickMsg after a short delay
func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}
