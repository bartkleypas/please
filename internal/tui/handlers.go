package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleTick manages the spinner frame index and keeps the animation alive
// while the LLM is "thinking" or while node creation animations are active.
func (m *Model) handleTick() (tea.Model, tea.Cmd) {
	m.SpinnerFrame++
	if m.SpinnerFrame >= len(spinnerFrames) {
		m.SpinnerFrame = 0
	}

	// Keep ticking if thinking OR if within the "pulse" animation window (5s after activity)
	if m.IsThinking || time.Since(m.LastActivity) < 5*time.Second {
		return m, tick()
	}
	return m, nil
}

// handleExportResult provides visual feedback for data export operations.
func (m *Model) handleExportResult(msg exportResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Export failed: %v", msg.err)
	} else {
		m.Notification = fmt.Sprintf("Successfully exported to %s", msg.filename)
	}
	return m, nil
}

// handleSyncResult refreshes the view after a manual synchronization.
func (m *Model) handleSyncResult(msg syncResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Sync failed: %v", msg.err)
	} else {
		m.Notification = "Graph synchronized with disk."
		m.updateViewportContent()
	}
	return m, nil
}
