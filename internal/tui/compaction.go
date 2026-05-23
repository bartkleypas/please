package tui

import (
	"context"
	"fmt"

	"github.com/bartkleypas/please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleCompactionFinished(msg compactionFinishedMsg) (tea.Model, tea.Cmd) {
	m.IsCompressing = false
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Compaction failed: %v", msg.err)
		return m, nil
	}

	m.Notification = "Branch compacted into Supernode."
	m.syncMapSelection()
	return m, nil
}

func (m *Model) getCompactionRange(leafID string) []string {
	path, err := m.Manager.GetPath(leafID)
	if err != nil {
		return nil
	}

	var rangeIDs []string
	// Traverse backwards from the selected leaf to find nodes to squash
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		// Stop if we hit a system prompt or a previous summary
		if n.Role == engine.RoleSystem || n.Role == engine.RoleSummary {
			break
		}
		rangeIDs = append([]string{n.ID}, rangeIDs...)
	}
	return rangeIDs
}

func (m *Model) runCompaction() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		node, err := m.Manager.CompactRange(ctx, m.Provider, m.CompactTargetIDs)
		return compactionFinishedMsg{node: node, err: err}
	}
}
