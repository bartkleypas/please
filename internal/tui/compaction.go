package tui

import (
	"context"
	"fmt"

	"github.com/bartkleypas/please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleCompactionFinished(msg compactionFinishedMsg) (tea.Model, tea.Cmd) {
	m.IsCompressing = false
	m.CompactTargetIDs = nil
	m.CompactDirective = ""

	if msg.err != nil {
		m.Notification = fmt.Sprintf("Compaction failed: %v", msg.err)
		return m, nil
	}

	if msg.node != nil {
		m.CurrentID = msg.node.ID
		if m.ViewMode == ModeChat {
			m.navigateToNode(msg.node)
		} else {
			m.syncMapSelection()
			m.ViewportOverride = m.generateMapString()
			m.Viewport.SetContent(m.ViewportOverride)
		}
	}

	m.Notification = "Branch compacted into Supernode."
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
	targetIDs := m.CompactTargetIDs
	directive := m.CompactDirective

	return func() tea.Msg {
		ctx := context.Background()

		// If connected to remote daemon, route through dedicated POST /api/v1/supernodes endpoint
		if remoteStorage, ok := m.Manager.Storage.(*engine.RemoteDaemonStorage); ok {
			node, err := remoteStorage.CreateSupernode(ctx, targetIDs, directive)
			if err != nil {
				return compactionFinishedMsg{node: nil, err: err}
			}
			// Refresh client-side graph
			_, _, _ = m.Manager.Sync()
			return compactionFinishedMsg{node: node, err: nil}
		}

		// Standalone mode
		node, err := m.Manager.CompactRangeWithDirective(ctx, m.Provider, targetIDs, directive)
		return compactionFinishedMsg{node: node, err: err}
	}
}
