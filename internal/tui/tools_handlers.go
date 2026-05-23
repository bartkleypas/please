package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleToolsExecuted(msg toolsExecutedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Tool execution failed: %v", msg.err)
		m.IsThinking = false
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.StreamCancel = cancel

	return m, m.resumeStreamCmd(ctx, msg.activeNodeID)
}

func (m *Model) executeToolsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		activeID := m.InterleavingNodeID

		for _, call := range m.PendingToolCalls {
			result, err := m.Manager.ExecuteToolCall(ctx, call)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			// Side-Channel Interleaving: Update the Assistant node directly
			err = m.Manager.UpdateAssistantObservations(activeID, call.ID, result)
			if err != nil {
				return toolsExecutedMsg{err: fmt.Errorf("failed to update observations: %w", err), activeNodeID: activeID}
			}
		}

		return toolsExecutedMsg{activeNodeID: activeID}
	}
}

func (m *Model) cancelToolsCmd() tea.Cmd {
	return func() tea.Msg {
		activeID := m.InterleavingNodeID

		for _, call := range m.PendingToolCalls {
			result := "Error: Tool call cancelled by user."
			err := m.Manager.UpdateAssistantObservations(activeID, call.ID, result)
			if err != nil {
				return toolsExecutedMsg{err: fmt.Errorf("failed to update observations: %w", err), activeNodeID: activeID}
			}
		}

		return toolsExecutedMsg{activeNodeID: activeID}
	}
}
