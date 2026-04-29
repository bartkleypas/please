package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Init() tea.Cmd {
	return nil
}

// Update is the main dispatcher for the Bubble Tea model. It delegates messages
// to specialized handler methods to maintain a flat and readable structure.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.handleTick()
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		return m.handleKeyEvent(msg)
	case llmStreamMsg:
		return m.handleLLMStream(msg)
	case llmStreamFinishedMsg:
		return m.handleLLMStreamFinished(msg)
	case streamResponseMsg:
		return m.handleStreamResponse(msg)
	case llmResponseMsg:
		return m.handleLLMResponse(msg)
	case syncResultMsg:
		return m.handleSyncResult(msg)
	case exportResultMsg:
		return m.handleExportResult(msg)
	}

	return m, nil
}
