package tui

import (
	"context"
	"time"

	"github.com/bartkleypas/please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func streamResponse(ctx context.Context, provider engine.LLMProvider, messages []engine.Message, tools []engine.Tool, parentID string, activeNodeID string) tea.Cmd {
	return func() tea.Msg {
		contentChan, thoughtChan, toolCallChan, errChan := provider.GenerateResponseStream(ctx, messages, tools)
		return streamResponseMsg{
			contentChan:  contentChan,
			thoughtChan:  thoughtChan,
			toolCallChan: toolCallChan,
			errChan:      errChan,
			parentID:     parentID,
			activeNodeID: activeNodeID,
		}
	}
}

func waitForStream(contentChan <-chan string, thoughtChan <-chan string, toolCallChan <-chan []engine.ToolCall, errChan <-chan error, parentID string, activeNodeID string) tea.Cmd {
	return func() tea.Msg {
		select {
		case content, ok := <-contentChan:
			if !ok {
				// Content closed, now wait for potential tool calls or error
				return checkRemainingChannels(toolCallChan, errChan, parentID, activeNodeID)
			}
			return llmStreamMsg{content: content, parentID: parentID, activeNodeID: activeNodeID}

		case thought, ok := <-thoughtChan:
			if !ok {
				// Thought closed, keep waiting for content
				return waitForStream(contentChan, nil, toolCallChan, errChan, parentID, activeNodeID)()
			}
			return llmThoughtStreamMsg{thought: thought, parentID: parentID, activeNodeID: activeNodeID}

		case tc := <-toolCallChan:
			return llmStreamFinishedMsg{parentID: parentID, activeNodeID: activeNodeID, toolCalls: tc}

		case err := <-errChan:
			return llmStreamFinishedMsg{err: err, parentID: parentID, activeNodeID: activeNodeID}

		default:
			// Priority to content, then thought, then others
			select {
			case content, ok := <-contentChan:
				if !ok {
					return checkRemainingChannels(toolCallChan, errChan, parentID, activeNodeID)
				}
				return llmStreamMsg{content: content, parentID: parentID, activeNodeID: activeNodeID}
			case thought, ok := <-thoughtChan:
				if !ok {
					return waitForStream(contentChan, nil, toolCallChan, errChan, parentID, activeNodeID)()
				}
				return llmThoughtStreamMsg{thought: thought, parentID: parentID, activeNodeID: activeNodeID}
			case tc := <-toolCallChan:
				return llmStreamFinishedMsg{parentID: parentID, activeNodeID: activeNodeID, toolCalls: tc}
			case err := <-errChan:
				return llmStreamFinishedMsg{err: err, parentID: parentID, activeNodeID: activeNodeID}
			default:
				// Fallback to blocking select
				select {
				case content, ok := <-contentChan:
					if !ok {
						return checkRemainingChannels(toolCallChan, errChan, parentID, activeNodeID)
					}
					return llmStreamMsg{content: content, parentID: parentID, activeNodeID: activeNodeID}
				case thought, ok := <-thoughtChan:
					if !ok {
						return waitForStream(contentChan, nil, toolCallChan, errChan, parentID, activeNodeID)()
					}
					return llmThoughtStreamMsg{thought: thought, parentID: parentID, activeNodeID: activeNodeID}
				case tc := <-toolCallChan:
					return llmStreamFinishedMsg{parentID: parentID, activeNodeID: activeNodeID, toolCalls: tc}
				case err := <-errChan:
					return llmStreamFinishedMsg{err: err, parentID: parentID, activeNodeID: activeNodeID}
				}
			}
		}
	}
}

func checkRemainingChannels(toolCallChan <-chan []engine.ToolCall, errChan <-chan error, parentID string, activeNodeID string) tea.Msg {
	var toolCalls []engine.ToolCall
	var streamErr error

	if tc, ok := <-toolCallChan; ok {
		toolCalls = tc
	}
	if err, ok := <-errChan; ok {
		streamErr = err
	}

	return llmStreamFinishedMsg{
		parentID:     parentID,
		activeNodeID: activeNodeID,
		toolCalls:    toolCalls,
		err:          streamErr,
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
