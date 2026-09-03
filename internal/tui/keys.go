package tui

import (
	"context"
	"fmt"

	"github.com/bartkleypas/please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

// handleWindowSize responds to terminal resize events by updating viewport
// dimensions and refreshing the wrapped chat history.
func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.Width = msg.Width
	m.Viewport.Width = msg.Width - 4    // Account for borders (2) and padding(2)
	m.Viewport.Height = msg.Height - 14 // Increased offset for textarea height
	m.TextInput.SetWidth(msg.Width - 4)
	m.TextInput.SetHeight(3) // Multi-line input
	m.updateViewportContent()
	return m, nil
}

// handleKeyEvent coordinates keyboard navigation and command execution.
// It ensures navigation keys are sent to the viewport while standard text
// is sent to the text input.
func (m *Model) handleKeyEvent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global exit handler
	if msg.String() == "ctrl+c" {
		if m.StreamCancel != nil {
			m.StreamCancel()
		}
		return m, tea.Quit
	}

	// 1. Let modal interceptors consume standard keys first
	if newM, cmd, handled := m.handleModalKeys(msg); handled {
		return newM, cmd
	}

	var cmd tea.Cmd

	// 2. Delegate key handling based on active ViewMode
	switch m.ViewMode {
	case ModeChat:
		var cCmd tea.Cmd
		var handled bool
		m, cCmd, handled = m.handleChatKeys(msg)
		if handled {
			return m, cCmd
		}
		cmd = tea.Batch(cmd, cCmd)
	case ModeMap:
		return m.handleMapKeys(msg)
	}

	// 3. Handle global and generic view overrides
	if m.ViewportOverride != "" {
		switch msg.String() {
		case "esc":
			m.ViewportOverride = ""
			m.updateViewportContent()
			return m, nil
		}
	}

	switch msg.String() {
	case "enter":
		return m.handleEnterKey()
	}

	var tiCmd tea.Cmd
	m.TextInput, tiCmd = m.TextInput.Update(msg)
	return m, tea.Batch(cmd, tiCmd)
}

// handleModalKeys checks if there is any active modal or overlay state that needs
// to intercept key events before they propagate to normal view mode handlers.
func (m *Model) handleModalKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	if newM, cmd, handled := m.handlePacingKeys(msg); handled {
		return newM, cmd, true
	}
	if newM, cmd, handled := m.handleCompactionConfirmKeys(msg); handled {
		return newM, cmd, true
	}
	if newM, cmd, handled := m.handlePruneConfirmKeys(msg); handled {
		return newM, cmd, true
	}
	if newM, cmd, handled := m.handleToolConfirmKeys(msg); handled {
		return newM, cmd, true
	}
	return m, nil, false
}

func (m *Model) handlePacingKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	if m.PacingActive {
		switch msg.String() {
		case "esc", "enter", "space":
			newM, cmd := m.skipPacing()
			return newM.(*Model), cmd, true
		}
	}
	return m, nil, false
}

func (m *Model) handleCompactionConfirmKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	if m.AwaitingCompactConfirmation {
		switch msg.String() {
		case "y", "Y":
			m.AwaitingCompactConfirmation = false
			m.IsCompressing = true
			return m, m.runCompaction(), true
		case "n", "N", "esc":
			m.AwaitingCompactConfirmation = false
			m.CompactTargetIDs = nil
			return m, nil, true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m *Model) handlePruneConfirmKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	if m.AwaitingPruneConfirmation {
		switch msg.String() {
		case "y", "Y":
			m.AwaitingPruneConfirmation = false
			err := m.Manager.PruneBranch(m.PruneTargetID)
			if err != nil {
				m.Notification = fmt.Sprintf("Prune failed: %v", err)
			} else {
				m.Notification = "Branch pruned."
			}
			m.syncMapSelection()
			return m, nil, true
		case "n", "N", "esc":
			m.AwaitingPruneConfirmation = false
			m.PruneTargetID = ""
			return m, nil, true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m *Model) handleToolConfirmKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	if m.AwaitingToolConfirmation && m.TextInput.Value() == "" {
		switch msg.String() {
		case "y", "Y":
			m.AwaitingToolConfirmation = false
			m.IsThinking = true
			return m, m.executeToolsCmd(), true
		case "n", "N":
			m.AwaitingToolConfirmation = false
			m.IsThinking = true
			return m, m.cancelToolsCmd(), true
		case "esc":
			m.AwaitingToolConfirmation = false
			m.PendingToolCalls = nil
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m *Model) handleChatKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	var cmd tea.Cmd

	switch msg.String() {
	case "tab":
		if m.ExpandedThoughts == nil {
			m.ExpandedThoughts = make(map[string]bool)
		}

		targetID := m.CurrentID
		if path, err := m.Manager.GetPath(m.CurrentID); err == nil && len(path) > 0 {
			for i := len(path) - 1; i >= 0; i-- {
				if path[i].Role == engine.RoleAssistant && (path[i].Thought != "" || (path[i].Metadata != nil && path[i].Metadata["segments"] != "")) {
					targetID = path[i].ID
					break
				}
			}
		}

		if targetID != "" {
			m.ExpandedThoughts[targetID] = !m.isThoughtExpanded(targetID)
			m.updateViewportContentPreservingOffset(m.Viewport.YOffset)
		}
		return m, nil, true

	case "shift+tab":
		if m.ExpandedThoughts == nil {
			m.ExpandedThoughts = make(map[string]bool)
		}

		hasExpanded := false
		if path, err := m.Manager.GetPath(m.CurrentID); err == nil {
			for _, node := range path {
				if node.Role == engine.RoleAssistant && m.isThoughtExpanded(node.ID) {
					hasExpanded = true
					break
				}
			}
		}

		newState := !hasExpanded
		if path, err := m.Manager.GetPath(m.CurrentID); err == nil {
			for _, node := range path {
				if node.Role == engine.RoleAssistant {
					m.ExpandedThoughts[node.ID] = newState
				}
			}
		}
		m.updateViewportContentPreservingOffset(m.Viewport.YOffset)
		return m, nil, true
	}

	// Only send specific scrolling keys to the viewport
	if msg.String() == "pgup" || msg.String() == "pgdown" || msg.String() == "up" || msg.String() == "down" {
		var vCmd tea.Cmd
		m.Viewport, vCmd = m.Viewport.Update(msg)
		cmd = tea.Batch(cmd, vCmd)
	}
	return m, cmd, false
}

func (m *Model) handleSearchKeys(msg tea.KeyMsg) (*Model, tea.Cmd, bool) {
	if m.Searching {
		switch msg.String() {
		case "enter":
			m.SearchQuery = m.SearchInput.Value()
			m.Searching = false
			m.ViewportOverride = m.generateMapString()
			m.Viewport.SetContent(m.ViewportOverride)
			return m, nil, true
		case "esc":
			m.Searching = false
			m.SearchInput.Reset()
			m.SearchQuery = ""
			m.ViewportOverride = m.generateMapString()
			m.Viewport.SetContent(m.ViewportOverride)
			return m, nil, true
		}
		var siCmd tea.Cmd
		m.SearchInput, siCmd = m.SearchInput.Update(msg)
		return m, siCmd, true
	}
	return m, nil, false
}

func (m *Model) handleMapKeys(msg tea.KeyMsg) (*Model, tea.Cmd) {
	// First let search mode intercept keys if active
	if newM, cmd, handled := m.handleSearchKeys(msg); handled {
		return newM, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.MapSelectionIndex > 0 {
			m.MapSelectionIndex--
			m.syncMapSelection()
		}
	case "down", "j":
		if m.MapSelectionIndex < len(m.MapNodeIDs)-1 {
			m.MapSelectionIndex++
			m.syncMapSelection()
		}
	case "h":
		m.ascendOrCollapseMap()
	case "l":
		m.descendOrUnfoldMap()
	case "g":
		// Snap to Root
		if len(m.MapNodeIDs) > 0 {
			m.MapSelectionIndex = 0
			m.syncMapSelection()
		}
	case "G":
		// Snap to current active Leaf node
		for i, id := range m.MapNodeIDs {
			if id == m.CurrentID {
				m.MapSelectionIndex = i
				m.syncMapSelection()
				break
			}
		}
	case "v":
		m.AuditMode = !m.AuditMode
		if m.AuditMode {
			m.Notification = "Audit Mode enabled."
		} else {
			m.Notification = "Audit Mode disabled."
		}
		m.ViewportOverride = m.generateMapString()
		m.Viewport.SetContent(m.ViewportOverride)
		return m, nil
	case "s":
		// Sync/Refresh
		m.ViewportOverride = m.generateMapString()
		m.Viewport.SetContent(m.ViewportOverride)
		m.Notification = "Map refreshed."
		return m, nil
	case "c":
		// Compress/Compact
		if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
			targetID := m.MapNodeIDs[m.MapSelectionIndex]
			rangeIDs := m.getCompactionRange(targetID)
			if len(rangeIDs) > 0 {
				m.CompactTargetIDs = rangeIDs
				m.AwaitingCompactConfirmation = true
				return m, nil
			} else {
				m.Notification = "Nothing to compress here."
			}
		}
	case "d", "delete":
		if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
			m.PruneTargetID = m.MapNodeIDs[m.MapSelectionIndex]
			m.AwaitingPruneConfirmation = true
			return m, nil
		}
	case "/":
		m.Searching = true
		m.SearchInput.Focus()
		return m, nil
	case "enter":
		if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
			targetID := m.MapNodeIDs[m.MapSelectionIndex]
			if node, err := m.Manager.GetNode(targetID); err == nil {
				m.navigateToNode(node)
				return m, nil
			}
		}
	case "esc":
		m.ViewMode = ModeChat
		m.ViewportOverride = ""
		m.updateViewportContent()
		return m, nil
	}

	// Allow standard scrolling in map mode too
	var vCmd tea.Cmd
	m.Viewport, vCmd = m.Viewport.Update(msg)
	return m, vCmd
}

func (m *Model) ascendOrCollapseMap() {
	if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
		currentID := m.MapNodeIDs[m.MapSelectionIndex]
		children := m.Manager.GetChildren(currentID)

		if len(children) > 0 && !m.CollapsedNodes[currentID] {
			// Node is expanded, so collapse it
			m.CollapsedNodes[currentID] = true
			m.syncMapSelection()
		} else {
			// Node is already collapsed or has no children, so ascend to parent
			node, _ := m.Manager.GetNode(currentID)
			if node != nil && node.ParentID != "" {
				for i, id := range m.MapNodeIDs {
					if id == node.ParentID {
						m.MapSelectionIndex = i
						m.syncMapSelection()
						break
					}
				}
			}
		}
	}
}

func (m *Model) descendOrUnfoldMap() {
	if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
		currentID := m.MapNodeIDs[m.MapSelectionIndex]
		children := m.Manager.GetChildren(currentID)

		if len(children) > 0 && m.CollapsedNodes[currentID] {
			// Node is collapsed, so unfold it
			delete(m.CollapsedNodes, currentID)
			m.syncMapSelection()
		} else if len(children) > 0 {
			// Node is already unfolded, so descend to first child
			for i := m.MapSelectionIndex + 1; i < len(m.MapNodeIDs); i++ {
				childNode, _ := m.Manager.GetNode(m.MapNodeIDs[i])
				if childNode != nil && childNode.ParentID == currentID {
					m.MapSelectionIndex = i
					m.syncMapSelection()
					break
				}
			}
		}
	}
}

// handleEnterKey processes the user's input based on the current application mode
// (Setup, Persona, Command, or Chat).
func (m *Model) handleEnterKey() (tea.Model, tea.Cmd) {
	if m.IsThinking {
		return m, nil
	}

	if m.StreamCancel != nil {
		m.StreamCancel()
		m.StreamCancel = nil
	}

	input := m.TextInput.Value()
	if input == "" {
		return m, nil
	}

	// 1. Handle Setup Modes: Initial system prompt or new persona creation.
	if m.SetupMode || m.PersonaSetupMode {
		// Genesis node holds pure persona without baked-in workspace supplement (ADR 003)
		newNode, err := m.Manager.CreateNode("", engine.RoleSystem, input, false)
		if err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		m.CurrentID = newNode.ID
		m.SetupMode = false
		m.PersonaSetupMode = false
		m.updateViewportWithNode(newNode)
		m.TextInput.Reset()
		return m, tea.Batch(tick())
	}

	// 2. Handle Commands: Intercept and execute slash commands.
	if newM, cmd, handled := m.HandleCommand(input); handled {
		return newM, cmd
	}

	// Handle Dialogue Intervention:
	// If the user submits a message while tool execution is pending,
	// cancel the pending tools and proceed with the new message.
	if m.AwaitingToolConfirmation {
		for _, call := range m.PendingToolCalls {
			result := "Error: Tool call cancelled by user."
			_ = m.Manager.UpdateAssistantObservations(m.InterleavingNodeID, call.ID, result)
		}
		m.AwaitingToolConfirmation = false
		m.PendingToolCalls = nil
		m.Notification = "Pending tools cancelled."
	}

	// 3. Handle Regular Chat: Create a user node and trigger LLM generation.
	newNode, err := m.Manager.CreateNode(m.CurrentID, engine.RoleUser, input, false)
	if err != nil {
		m.Notification = fmt.Sprintf("Error: %v", err)
		m.TextInput.Reset()
		return m, nil
	}
	if len(m.PendingImages) > 0 {
		m.Manager.AttachImages(newNode, m.PendingImages)
		m.PendingImages = nil
		_ = m.Manager.Storage.SaveNode(newNode)
	}
	m.CurrentID = newNode.ID
	m.updateViewportWithNode(newNode)

	m.TextInput.Reset()
	m.IsThinking = true

	messages, err := m.Manager.BuildLLMContext(m.CurrentID, m.Config.SupportsVision())
	if err != nil {
		m.Notification = fmt.Sprintf("Error building context: %v", err)
		m.IsThinking = false
		return m, nil
	}

	// Trigger asynchronous generation.
	m.IsThinking = true
	ctx, cancel := context.WithCancel(context.Background())
	m.StreamCancel = cancel

	return m, tea.Batch(
		streamResponse(ctx, m.Provider, messages, m.Manager.Registry.GetToolsForPolicy(m.Config.GetSandboxPolicy()), newNode.ID, ""),
		tick(),
	)
}
