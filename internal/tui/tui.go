package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Init() tea.Cmd {
	if m.RemoteURL != "" {
		token := ""
		caCert := ""
		if m.Config != nil && m.Config.Client != nil {
			token = m.Config.Client.AuthToken
			caCert = m.Config.Client.CACertPath
		}
		return listenRemoteEventsCmd(m.RemoteURL, token, caCert)
	}
	return nil
}

// Update is the main dispatcher for the Bubble Tea model. It delegates messages
// to specialized handler methods to maintain a flat and readable structure.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.handleTick()
	case pacingTickMsg:
		return m.handlePacingTick()
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		return m.handleKeyEvent(msg)
	case llmStreamMsg:
		return m.handleLLMStream(msg)
	case llmThoughtStreamMsg:
		return m.handleLLMThoughtStream(msg)
	case llmStreamFinishedMsg:
		return m.handleLLMStreamFinished(msg)
	case streamResponseMsg:
		return m.handleStreamResponse(msg)

	case remoteDaemonStreamConnMsg:
		m.RemoteEventsChan = msg.EventChan
		m.RemoteEventsCancel = msg.Cancel
		return m, waitForRemoteEventCmd(m.RemoteEventsChan)

	case remoteDaemonEventMsg:
		return m.handleRemoteDaemonEvent(msg)

	case syncResultMsg:
		return m.handleSyncResult(msg)
	case toolsExecutedMsg:
		return m.handleToolsExecuted(msg)
	case exportResultMsg:
		return m.handleExportResult(msg)
	case compactionFinishedMsg:
		return m.handleCompactionFinished(msg)
	}

	return m, nil
}
