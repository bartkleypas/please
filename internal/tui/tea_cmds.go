package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func waitForStream(contentChan <-chan string, errChan <-chan error, parentID string) tea.Cmd {
	return func() tea.Msg {
		select {
		case content, ok := <-contentChan:
			if !ok {
				return llmStreamFinishedMsg{parentID: parentID}
			}
			return llmStreamMsg{content: content, parentID: parentID}
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
