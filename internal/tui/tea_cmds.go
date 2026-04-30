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
		contentChan, thoughtChan, toolCallChan, errChan := provider.GenerateResponseStream(ctx, messages, tools)
		return streamResponseMsg{
			contentChan:  contentChan,
			thoughtChan:  thoughtChan,
			toolCallChan: toolCallChan,
			errChan:      errChan,
			parentID:     parentID,
		}
	}
}

func waitForStream(contentChan <-chan string, thoughtChan <-chan string, toolCallChan <-chan []engine.ToolCall, errChan <-chan error, parentID string) tea.Cmd {
	return func() tea.Msg {
		select {
		case content, ok := <-contentChan:
			if !ok {
				// Content closed, now wait for potential tool calls or error
				return checkRemainingChannels(toolCallChan, errChan, parentID)
			}
			return llmStreamMsg{content: content, parentID: parentID}

		case thought, ok := <-thoughtChan:
			if !ok {
				// Thought closed, keep waiting for content
				return waitForStream(contentChan, nil, toolCallChan, errChan, parentID)()
			}
			return llmThoughtStreamMsg{thought: thought, parentID: parentID}

		case tc := <-toolCallChan:
			return llmStreamFinishedMsg{parentID: parentID, toolCalls: tc}

		case err := <-errChan:
			return llmStreamFinishedMsg{err: err, parentID: parentID}

		default:
			// Priority to content, then thought, then others
			select {
			case content, ok := <-contentChan:
				if !ok {
					return checkRemainingChannels(toolCallChan, errChan, parentID)
				}
				return llmStreamMsg{content: content, parentID: parentID}
			case thought, ok := <-thoughtChan:
				if !ok {
					return waitForStream(contentChan, nil, toolCallChan, errChan, parentID)()
				}
				return llmThoughtStreamMsg{thought: thought, parentID: parentID}
			case tc := <-toolCallChan:
				return llmStreamFinishedMsg{parentID: parentID, toolCalls: tc}
			case err := <-errChan:
				return llmStreamFinishedMsg{err: err, parentID: parentID}
			default:
				// Fallback to blocking select
				select {
				case content, ok := <-contentChan:
					if !ok {
						return checkRemainingChannels(toolCallChan, errChan, parentID)
					}
					return llmStreamMsg{content: content, parentID: parentID}
				case thought, ok := <-thoughtChan:
					if !ok {
						return waitForStream(contentChan, nil, toolCallChan, errChan, parentID)()
					}
					return llmThoughtStreamMsg{thought: thought, parentID: parentID}
				case tc := <-toolCallChan:
					return llmStreamFinishedMsg{parentID: parentID, toolCalls: tc}
				case err := <-errChan:
					return llmStreamFinishedMsg{err: err, parentID: parentID}
				}
			}
		}
	}
}

func checkRemainingChannels(toolCallChan <-chan []engine.ToolCall, errChan <-chan error, parentID string) tea.Msg {
	select {
	case tc := <-toolCallChan:
		return llmStreamFinishedMsg{parentID: parentID, toolCalls: tc}
	case err := <-errChan:
		return llmStreamFinishedMsg{parentID: parentID, err: err}
	default:
		return llmStreamFinishedMsg{parentID: parentID}
	}
}

func syncVault(mgr *engine.Manager) tea.Cmd {
	return func() tea.Msg {
		_, _, err := mgr.Sync()
		return syncResultMsg{err: err}
	}
}

// tick is a command that sends a tickMsg after a short delay
func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}
