package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleStreamResponse(streamMsg streamResponseMsg) (tea.Model, tea.Cmd) {
	m.StreamContentChan = streamMsg.contentChan
	m.StreamThoughtChan = streamMsg.thoughtChan
	m.StreamToolCallChan = streamMsg.toolCallChan
	m.StreamErrChan = streamMsg.errChan
	m.StreamApprovalReqChan = streamMsg.approvalReqChan
	return m, tea.Batch(
		waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, m.StreamApprovalReqChan, streamMsg.parentID, streamMsg.activeNodeID),
		tick(),
	)
}

// handleLLMStream appends incoming chunks to the streaming buffer and refreshes the view.
func (m *Model) handleLLMStream(msg llmStreamMsg) (tea.Model, tea.Cmd) {
	// Keep IsThinking true while streaming to maintain the spinner/animation
	m.IsThinking = true

	if m.Config.EnableNaturalPacing() && !m.PacingSkipped {
		m.PacingBuffer = append(m.PacingBuffer, []rune(msg.content)...)
		var cmd tea.Cmd
		if !m.PacingActive {
			m.PacingActive = true
			cmd = pacingTick(0)
		}
		return m, tea.Batch(
			cmd,
			waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, m.StreamApprovalReqChan, msg.parentID, msg.activeNodeID),
		)
	}

	m.CurrentStreamingContent += msg.content
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, m.StreamApprovalReqChan, msg.parentID, msg.activeNodeID)
}

// handleLLMThoughtStream appends incoming reasoning chunks to the streaming thought buffer.
func (m *Model) handleLLMThoughtStream(msg llmThoughtStreamMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = true
	m.CurrentStreamingThought += msg.thought
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, m.StreamApprovalReqChan, msg.parentID, msg.activeNodeID)
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
		if m.Config.EnableBellOnTurnComplete() {
			return m, BellCmd()
		}
		return m, nil
	}

	// Driven by canonical SessionHarness (in-process LocalHarnessProvider or remote daemon),
	// which autonomously executes tools, records observations, and persists turns.
	_, lastID, _ := m.Manager.Sync()
	if m.SessionID != "" && m.Manager != nil && m.Manager.Storage != nil {
		if headID, err := m.Manager.Storage.GetSessionHead(m.SessionID); err == nil && headID != "" && headID != msg.parentID {
			m.CurrentID = headID
		} else if lastID != "" && lastID != msg.parentID {
			m.CurrentID = lastID
			_ = m.Manager.Storage.SaveSessionHead(m.SessionID, m.CurrentID)
		} else if m.CurrentStreamingContent != "" || m.CurrentStreamingThought != "" {
			if botNode, err := m.Manager.CreateAssistantNode(msg.parentID, m.CurrentStreamingContent, m.CurrentStreamingThought, msg.toolCalls, false); err == nil {
				m.CurrentID = botNode.ID
				_ = m.Manager.Storage.SaveSessionHead(m.SessionID, m.CurrentID)
			}
		}
	} else if lastID != "" && lastID != msg.parentID {
		m.CurrentID = lastID
	} else if m.CurrentStreamingContent != "" || m.CurrentStreamingThought != "" {
		if botNode, err := m.Manager.CreateAssistantNode(msg.parentID, m.CurrentStreamingContent, m.CurrentStreamingThought, msg.toolCalls, false); err == nil {
			m.CurrentID = botNode.ID
		}
	}
	m.LastActivity = time.Now()
	m.CurrentStreamingContent = ""
	m.CurrentStreamingThought = ""
	m.updateViewportContent()
	cmds := []tea.Cmd{tick()}
	if m.Config.EnableBellOnTurnComplete() {
		cmds = append(cmds, BellCmd())
	}
	return m, tea.Batch(cmds...)
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
