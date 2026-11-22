package tui

import (
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

type JumpCommand struct{}

func (c *JumpCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /jump <id_prefix>"
		return m, nil
	}

	prefix := args[0]
	if node, err := m.Manager.FindNodeByShortID(prefix); err == nil {
		m.navigateToNode(node)
		return m, nil
	}

	m.Notification = fmt.Sprintf("Error: No node matching %s", prefix)
	return m, nil
}

type ListCommand struct{}

func (c *ListCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	nodes := m.Manager.GetAllNodeIDs()
	var content string
	if len(nodes) == 0 {
		content = "No nodes found in graph."
	} else {
		content = "--- Node List ---\n" + strings.Join(nodes, "\n")
	}
	m.ensureViewStack()
	m.ViewStack.Push(NewTextViewLayer(m, "nodes", "Node List", content, ""))
	return m, nil
}

type MarkCommand struct{}

func (c *MarkCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	targetID := m.CurrentID
	if len(args) > 0 {
		targetID = args[0]
	}

	if node, err := m.Manager.GetNode(targetID); err == nil {
		if err := m.Manager.SetBookmark(node.ID, true); err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
		} else {
			m.Notification = fmt.Sprintf("Node %s bookmarked!", targetID)
		}
	} else {
		m.Notification = "Error: Node not found"
	}
	return m, nil
}

type UnmarkCommand struct{}

func (c *UnmarkCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /unmark <id>"
		return m, nil
	}

	targetID := args[0]
	if node, err := m.Manager.GetNode(targetID); err == nil {
		if err := m.Manager.SetBookmark(node.ID, false); err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
		} else {
			m.Notification = "Node " + targetID + " unbookmarked!"
		}
	} else {
		m.Notification = "Error: Node not found"
	}
	return m, nil
}

type PersonaCommand struct{}

func (c *PersonaCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.PersonaSetupMode = true
	return m, nil
}

type MapCommand struct{}

func (c *MapCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.ViewMode = ModeMap
	m.MapSelectionIndex = 0
	m.syncViewportDimensions()
	m.Viewport.SetContent(m.generateMapString())
	m.Viewport.GotoTop()
	m.ensureViewStack()
	if m.ViewStack.Top().Name() != "map" {
		m.ViewStack.Push(newMapLayer(m))
	}
	return m, nil
}

type CompactCommand struct{}

func (c *CompactCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	rangeIDs := m.getCompactionRange(m.CurrentID)
	if len(rangeIDs) == 0 {
		m.Notification = "Nothing to compact on current branch."
		return m, nil
	}

	directive := strings.Join(args, " ")
	m.CompactTargetIDs = rangeIDs
	m.CompactDirective = directive
	m.IsCompressing = true
	m.Notification = "Compacting branch into Supernode..."
	return m, m.runCompaction()
}

type SessionCommand struct{}

func (c *SessionCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		headInfo := m.CurrentID
		if len(headInfo) > 8 {
			headInfo = headInfo[:8]
		}
		m.Notification = fmt.Sprintf("Active session: %q (head: %s)", m.SessionID, headInfo)
		return m, nil
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		if m.Manager == nil || m.Manager.Storage == nil {
			m.Notification = "Storage not initialized"
			return m, nil
		}
		sessions, err := m.Manager.Storage.ListSessions()
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to list sessions: %v", err)
			return m, nil
		}
		var content string
		if len(sessions) == 0 {
			content = "--- Sessions ---\nNo saved sessions found."
		} else {
			var sb strings.Builder
			sb.WriteString("--- Active Sessions ---\n")
			for id, headID := range sessions {
				marker := "  "
				if id == m.SessionID {
					marker = "➜ "
				}
				shortHead := headID
				if len(shortHead) > 8 {
					shortHead = shortHead[:8]
				}
				sb.WriteString(fmt.Sprintf("%s%-16s (head: %s)\n", marker, id, shortHead))
			}
			content = sb.String()
		}
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "sessions", "Active Sessions", content, ""))
		return m, nil

	case "switch":
		if len(args) < 2 {
			m.Notification = "Usage: /session switch <name>"
			return m, nil
		}
		targetSession := args[1]
		if targetSession == "" {
			m.Notification = "Session name cannot be empty"
			return m, nil
		}

		m.SessionID = targetSession
		if rdp, ok := m.Provider.(*providers.RemoteDaemonProvider); ok {
			rdp.SessionID = targetSession
		}
		if lhp, ok := m.Provider.(*engine.LocalHarnessProvider); ok {
			lhp.SetSessionID(targetSession)
		}
		if rds, ok := m.Manager.Storage.(*storage.RemoteDaemonStorage); ok {
			rds.SessionID = targetSession
		}

		headID, err := m.Manager.Storage.GetSessionHead(targetSession)
		if err == nil && headID != "" {
			if node, err := m.Manager.GetNode(headID); err == nil {
				m.navigateToNode(node)
				shortID := headID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				m.Notification = fmt.Sprintf("Switched to session %q (resumed at %s)", targetSession, shortID)
				return m, nil
			}
		}

		_ = m.Manager.Storage.SaveSessionHead(targetSession, m.CurrentID)
		m.Notification = fmt.Sprintf("Switched to session %q (anchored at current node)", targetSession)
		return m, nil

	default:
		// Shortcut: `/session <name>` behaves like `/session switch <name>`
		targetSession := args[0]
		m.SessionID = targetSession
		if rdp, ok := m.Provider.(*providers.RemoteDaemonProvider); ok {
			rdp.SessionID = targetSession
		}
		if lhp, ok := m.Provider.(*engine.LocalHarnessProvider); ok {
			lhp.SetSessionID(targetSession)
		}
		if rds, ok := m.Manager.Storage.(*storage.RemoteDaemonStorage); ok {
			rds.SessionID = targetSession
		}

		headID, err := m.Manager.Storage.GetSessionHead(targetSession)
		if err == nil && headID != "" {
			if node, err := m.Manager.GetNode(headID); err == nil {
				m.navigateToNode(node)
				shortID := headID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				m.Notification = fmt.Sprintf("Switched to session %q (resumed at %s)", targetSession, shortID)
				return m, nil
			}
		}

		_ = m.Manager.Storage.SaveSessionHead(targetSession, m.CurrentID)
		m.Notification = fmt.Sprintf("Switched to session %q (anchored at current node)", targetSession)
		return m, nil
	}
}
