package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func (m *Model) generateSystemSupplement() string {
	var sb strings.Builder
	sb.WriteString("\n\n### PROJECT CONTEXT (AUTOMATED DISCOVERY)\n")

	wsDir := "."
	if m.Config != nil {
		wsDir = m.Config.GetWorkspaceDir()
	}

	// 1. Shallow Directory Listing
	entries, err := os.ReadDir(wsDir)
	if err == nil {
		sb.WriteString("Current Directory Tree:\n")
		for _, e := range entries {
			if e.IsDir() {
				sb.WriteString(" - ")
				sb.WriteString(e.Name())
				sb.WriteString("/\n")
			} else {
				sb.WriteString(" - ")
				sb.WriteString(e.Name())
				sb.WriteString("\n")
			}
		}
	}

	// 2. Index Header
	indexContent, err := os.ReadFile(filepath.Join(wsDir, "index.md"))
	if err == nil {
		lines := strings.Split(string(indexContent), "\n")
		limit := 25
		if len(lines) < limit {
			limit = len(lines)
		}
		sb.WriteString("\nIndex Snippet:\n")
		sb.WriteString(strings.Join(lines[:limit], "\n"))
	}

	// 3. Turn Signatures (Signats)
	sb.WriteString("\n\n### TURN SIGNATURES (SIGNATS)\n")
	sb.WriteString("Conclude your final response with a 1-3 emoji signature (signat) summarizing the domain and mood of your turn (e.g. 🛠️💻 for code/impl, 🧠📐 for logic/math, 🔍📁 for research/inspection, 🎨✨ for design/styling, 📝💡 for ideation).\n")

	return sb.String()
}
