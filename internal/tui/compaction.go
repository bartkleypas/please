package tui

import (
	"context"
	"fmt"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/storage"

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
		if m.SessionID != "" && m.Manager != nil && m.Manager.Storage != nil {
			_ = m.Manager.Storage.SaveSessionHead(m.SessionID, m.CurrentID)
		}
		if m.ViewStack == nil || m.ViewStack.Top() == nil || m.ViewStack.Top().Name() == "chat" {
			m.navigateToNode(msg.node)
		} else {
			m.syncMapSelection()
			m.Viewport.SetContent(m.generateMapString())
		}
	}

	if msg.node != nil && msg.node.Metadata != nil && msg.node.Metadata["memories_harvested"] != "" && msg.node.Metadata["memories_harvested"] != "0" {
		count := msg.node.Metadata["memories_harvested"]
		memWord := "memories"
		if count == "1" {
			memWord = "memory"
		}
		m.Notification = fmt.Sprintf("Branch compacted into Supernode (🧠 harvested %s workspace %s).", count, memWord)
	} else {
		m.Notification = "Branch compacted into Supernode."
	}
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
		if n.Role == domain.RoleSystem || n.Role == domain.RoleSummary {
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
		if remoteStorage, ok := m.Manager.Storage.(*storage.RemoteDaemonStorage); ok {
			node, err := remoteStorage.CreateSupernode(ctx, targetIDs, directive)
			if err != nil {
				return compactionFinishedMsg{node: nil, err: err}
			}
			// Refresh client-side graph
			_, _, _ = m.Manager.Sync()
			return compactionFinishedMsg{node: node, err: nil}
		}

		// Standalone mode: pass raw underlying provider for one-shot summary generation
		provider := m.Provider
		if lh, ok := m.Provider.(*engine.LocalHarnessProvider); ok && lh.RawProvider() != nil {
			provider = lh.RawProvider()
		}
		node, err := m.Manager.CompactRangeWithDirective(ctx, provider, targetIDs, directive)
		return compactionFinishedMsg{node: node, err: err}
	}
}
