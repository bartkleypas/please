package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleStreamResponse(streamMsg streamResponseMsg) (tea.Model, tea.Cmd) {
	m.StreamContentChan = streamMsg.contentChan
	m.StreamThoughtChan = streamMsg.thoughtChan
	m.StreamToolCallChan = streamMsg.toolCallChan
	m.StreamErrChan = streamMsg.errChan
	m.InterleavingNodeID = streamMsg.activeNodeID
	return m, tea.Batch(
		waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, streamMsg.parentID, streamMsg.activeNodeID),
		tick(),
	)
}

// handleLLMStream appends incoming chunks to the streaming buffer and refreshes the view.
func (m *Model) handleLLMStream(msg llmStreamMsg) (tea.Model, tea.Cmd) {
	// Keep IsThinking true while streaming to maintain the spinner/animation
	m.IsThinking = true

	if m.Config.IsPacingEnabled() && !m.PacingSkipped {
		m.PacingBuffer = append(m.PacingBuffer, []rune(msg.content)...)
		var cmd tea.Cmd
		if !m.PacingActive {
			m.PacingActive = true
			cmd = pacingTick(0)
		}
		return m, tea.Batch(
			cmd,
			waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID, msg.activeNodeID),
		)
	}

	m.CurrentStreamingContent += msg.content
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID, msg.activeNodeID)
}

// handleLLMThoughtStream appends incoming reasoning chunks to the streaming thought buffer.
func (m *Model) handleLLMThoughtStream(msg llmThoughtStreamMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = true
	m.CurrentStreamingThought += msg.thought
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID, msg.activeNodeID)
}

// handleLLMStreamFinished commits the full streamed response to the graph as a new node or updates existing.
func (m *Model) handleLLMStreamFinished(msg llmStreamFinishedMsg) (tea.Model, tea.Cmd) {
	if m.PacingActive {
		m.FinishedMsgPending = &msg
		m.LLMFinished = true
		return m, nil
	}

	m.PacingSkipped = false
	m.LLMFinished = false
	m.FinishedMsgPending = nil

	m.IsThinking = false
	if m.StreamCancel != nil {
		m.StreamCancel()
		m.StreamCancel = nil
	}

	if msg.err != nil {
		m.Notification = fmt.Sprintf("Error: %v", msg.err)
		m.CurrentStreamingContent = ""
		m.CurrentStreamingThought = ""
		return m, nil
	}

	var activeID string
	if msg.activeNodeID != "" {
		// Update existing node
		node, err := m.Manager.GetNode(msg.activeNodeID)
		if err != nil {
			m.Notification = fmt.Sprintf("Error finding node: %v", err)
			return m, nil
		}
		node.Content += m.CurrentStreamingContent
		node.Thought += m.CurrentStreamingThought
		node.ToolCalls = append(node.ToolCalls, msg.toolCalls...)

		// Update segments in metadata for causal history reconstruction
		type AssistantSegment struct {
			Content string `json:"content"`
			Thought string `json:"thought"`
		}
		var segments []AssistantSegment
		if node.Metadata == nil {
			node.Metadata = make(map[string]string)
		}
		if segStr, ok := node.Metadata["segments"]; ok && segStr != "" {
			_ = json.Unmarshal([]byte(segStr), &segments)
		}
		segments = append(segments, AssistantSegment{
			Content: m.CurrentStreamingContent,
			Thought: m.CurrentStreamingThought,
		})
		if segJSON, err := json.Marshal(segments); err == nil {
			node.Metadata["segments"] = string(segJSON)
		}

		// Re-persist existing node state (SaveNode is now INSERT OR REPLACE)
		if err := m.Manager.Storage.SaveNode(node); err != nil {
			m.Notification = fmt.Sprintf("Error saving node: %v", err)
		}
		activeID = msg.activeNodeID
	} else {
		// Create assistant node as child of the designated parent (preserving continuity)
		botNode, err := m.Manager.CreateAssistantNode(msg.parentID, m.CurrentStreamingContent, m.CurrentStreamingThought, msg.toolCalls, false)
		if err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
			m.CurrentStreamingContent = ""
			m.CurrentStreamingThought = ""
			return m, nil
		}
		activeID = botNode.ID
	}

	m.CurrentID = activeID
	m.LastActivity = time.Now()
	m.updateViewportContent() // Full refresh to show final formatted node
	m.CurrentStreamingContent = ""
	m.CurrentStreamingThought = ""
	m.InterleavingNodeID = ""

	if len(msg.toolCalls) > 0 {
		m.PendingToolCalls = msg.toolCalls
		m.InterleavingNodeID = activeID // Mark this node for interleaving

		// Check if all tool calls are non-interactive
		allNonInteractive := true
		for _, call := range msg.toolCalls {
			if tool, ok := m.Manager.Registry.Tools[call.Function.Name]; ok {
				if tool.Interactive {
					allNonInteractive = false
					break
				}
			} else {
				// Unknown tools default to interactive for safety
				allNonInteractive = false
				break
			}
		}

		if allNonInteractive {
			m.IsThinking = true
			return m, m.executeToolsCmd()
		}

		m.AwaitingToolConfirmation = true
	}

	return m, tea.Batch(tick())
}

// handlePacingTick pops a rune from the pacing buffer and updates the viewport.
func (m *Model) handlePacingTick() (tea.Model, tea.Cmd) {
	if !m.PacingActive {
		return m, nil
	}

	if len(m.PacingBuffer) == 0 {
		if m.LLMFinished {
			m.PacingActive = false
			if m.FinishedMsgPending != nil {
				msg := *m.FinishedMsgPending
				m.FinishedMsgPending = nil
				return m.handleLLMStreamFinished(msg)
			}
			return m, nil
		}
		// Generator is running slower than playback speed, pause pacing loop temporarily.
		m.PacingActive = false
		return m, nil
	}

	// Pop a rune from the pacing buffer
	r := m.PacingBuffer[0]
	m.PacingBuffer = m.PacingBuffer[1:]
	m.CurrentStreamingContent += string(r)
	m.updateViewportWithStreaming()

	// Compute delay for the next tick based on punctuation
	delay := m.getPacingDelay(r, m.PacingBuffer)
	return m, pacingTick(delay)
}

// getPacingDelay computes the duration to pause after printing a specific rune.
func (m *Model) getPacingDelay(current rune, next []rune) time.Duration {
	baseDelay := 15 * time.Millisecond

	switch current {
	case '.', '!', '?':
		// If the next character is also punctuation/period (e.g. ellipsis "..."), do not pause long
		if len(next) > 0 && (next[0] == '.' || next[0] == '!' || next[0] == '?') {
			return baseDelay
		}
		return 300 * time.Millisecond
	case ':', ';':
		return 150 * time.Millisecond
	case ',':
		return 100 * time.Millisecond
	case '\n':
		return 200 * time.Millisecond
	case ' ':
		return 25 * time.Millisecond
	default:
		return baseDelay
	}
}

// skipPacing immediately flushes all buffered pacing content to the viewport and finalizes if complete.
func (m *Model) skipPacing() (tea.Model, tea.Cmd) {
	if !m.PacingActive {
		return m, nil
	}

	m.PacingSkipped = true
	m.PacingActive = false

	if len(m.PacingBuffer) > 0 {
		m.CurrentStreamingContent += string(m.PacingBuffer)
		m.PacingBuffer = nil
	}

	m.updateViewportWithStreaming()

	if m.LLMFinished && m.FinishedMsgPending != nil {
		msg := *m.FinishedMsgPending
		m.FinishedMsgPending = nil
		return m.handleLLMStreamFinished(msg)
	}

	return m, nil
}

func (m *Model) resumeStreamCmd(ctx context.Context, activeNodeID string) tea.Cmd {
	return func() tea.Msg {
		messages, err := m.Manager.BuildLLMContext(activeNodeID)
		if err != nil {
			return llmStreamFinishedMsg{err: err, activeNodeID: activeNodeID}
		}

		contentChan, thoughtChan, toolCallChan, errChan := m.Provider.GenerateResponseStream(ctx, messages, m.Manager.Registry.GetTools())
		return streamResponseMsg{
			contentChan:  contentChan,
			thoughtChan:  thoughtChan,
			toolCallChan: toolCallChan,
			errChan:      errChan,
			parentID:     "", // Parent is irrelevant during interleaving resumption
			activeNodeID: activeNodeID,
		}
	}
}
