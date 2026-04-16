package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Command defines the interface for TUI commands
type Command interface {
	Execute(m *Model, args []string) (tea.Model, tea.Cmd)
}

// commandRegistry maps command names to their implementation
var commandRegistry = make(map[string]Command)

func init() {
	commandRegistry["/jump"] = &JumpCommand{}
	commandRegistry["/list"] = &ListCommand{}
	commandRegistry["/mark"] = &MarkCommand{}
	commandRegistry["/unmark"] = &UnmarkCommand{}
	commandRegistry["/persona"] = &PersonaCommand{}
	commandRegistry["/map"] = &MapCommand{}
	commandRegistry["/q"] = &QuitCommand{}
	commandRegistry["/quit"] = &QuitCommand{}
	commandRegistry["/bye"] = &QuitCommand{}
}

type JumpCommand struct{}

func (c *JumpCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /jump <id_prefix>"
		m.TextInput.SetValue("")
		return m, nil
	}

	prefix := args[0]
	if node, err := m.Manager.FindNodeByPrefix(prefix); err == nil {
		m.CurrentID = node.ID
		m.Notification = fmt.Sprintf("Jumped to %s", node.ID)
		m.TextInput.SetValue("")
		path, _ := m.Manager.GetPath(node.ID)
		m.updateViewportWithJump(path)
		return m, nil
	}

	m.Notification = fmt.Sprintf("Error: No node matching %s", prefix)
	m.TextInput.SetValue("")
	return m, nil
}

type ListCommand struct{}

func (c *ListCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	nodes := m.Manager.GetAllNodeIDs()
	if len(nodes) == 0 {
		m.ViewportOverride = "No nodes found in graph."
	} else {
		m.ViewportOverride = "--- Node List ---\n" + strings.Join(nodes, "\n")
	}
	m.Viewport.SetContent(m.ViewportOverride)
	m.Viewport.GotoBottom()
	m.TextInput.SetValue("")
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
	m.TextInput.SetValue("")
	return m, nil
}

type UnmarkCommand struct{}

func (c *UnmarkCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /unmark <id>"
		m.TextInput.SetValue("")
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
	m.TextInput.SetValue("")
	return m, nil
}

type PersonaCommand struct{}

func (c *PersonaCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.PersonaSetupMode = true
	m.TextInput.SetValue("")
	return m, nil
}

type MapCommand struct{}

func (c *MapCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.ViewportOverride = m.generateMapString()
	m.Viewport.SetContent(m.ViewportOverride)
	m.Viewport.GotoBottom()
	m.TextInput.SetValue("")
	return m, nil
}

type QuitCommand struct{}

func (c *QuitCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	return m, tea.Quit
}
