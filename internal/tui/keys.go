package tui

import (
	"context"
	"fmt"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/storage"

	tea "github.com/charmbracelet/bubbletea"
)

// handleWindowSize responds to terminal resize events by updating viewport
// dimensions and refreshing the wrapped chat history.
func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.Width = msg.Width
	m.Height = msg.Height
	m.Viewport.Width = msg.Width - 4    // Account for borders (2) and padding(2)
	m.Viewport.Height = msg.Height - 14 // Increased offset for textarea height
	m.TextInput.SetWidth(msg.Width - 4)
	m.TextInput.SetHeight(3) // Multi-line input
	m.updateViewportContent()
	if m.ViewStack != nil {
		m.ViewStack.Update(msg)
	}
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

	m.ensureViewStack()

	for _, layer := range m.ViewStack.layers {
		if aware, ok := layer.(interface{ setModel(*Model) }); ok {
			aware.setModel(m)
		}
	}
	cmd, handled := m.ViewStack.Update(msg)
	if handled {
		return m, cmd
	}

	return m, nil
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
				if path[i].Role == domain.RoleAssistant && (path[i].Thought != "" || (path[i].Metadata != nil && path[i].Metadata["segments"] != "")) {
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
				if node.Role == domain.RoleAssistant && m.isThoughtExpanded(node.ID) {
					hasExpanded = true
					break
				}
			}
		}

		newState := !hasExpanded
		if path, err := m.Manager.GetPath(m.CurrentID); err == nil {
			for _, node := range path {
				if node.Role == domain.RoleAssistant {
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

	// 'G' snaps to current active leaf node
	if msg.String() == "G" {
		for i, id := range m.MapNodeIDs {
			if id == m.CurrentID {
				m.MapSelectionIndex = i
				m.syncMapSelection()
				break
			}
		}
		return m, nil
	}

	nav := &ListNavigator{
		Cursor:   m.MapSelectionIndex,
		Total:    len(m.MapNodeIDs),
		PageSize: 5,
	}
	if nav.HandleKey(msg.String()) {
		m.MapSelectionIndex = nav.Cursor
		m.syncMapSelection()
		return m, nil
	}

	switch msg.String() {
	case "h":
		m.ascendOrCollapseMap()
	case "l":
		m.descendOrUnfoldMap()
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
				m.ensureViewStack()
				m.ViewStack.Push(NewCompactConfirmOverlay(m.ViewStack, len(rangeIDs), func() tea.Cmd {
					m.IsCompressing = true
					return m.runCompaction()
				}, func() tea.Cmd {
					m.CompactTargetIDs = nil
					m.CompactDirective = ""
					m.Notification = "Compaction cancelled."
					return nil
				}))
				return m, nil
			} else {
				m.Notification = "Nothing to compress here."
			}
		}
	case "d", "delete":
		if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
			m.PruneTargetID = m.MapNodeIDs[m.MapSelectionIndex]
			targetID := m.PruneTargetID
			m.ensureViewStack()
			m.ViewStack.Push(NewPruneConfirmOverlay(m.ViewStack, targetID, func() tea.Cmd {
				m.PruneTargetID = ""
				if err := m.Manager.PruneBranch(targetID); err != nil {
					m.Notification = fmt.Sprintf("Prune failed: %v", err)
				} else {
					m.Notification = fmt.Sprintf("Branch %s pruned.", targetID)
					m.syncMapSelection()
					m.ViewportOverride = m.generateMapString()
					m.Viewport.SetContent(m.ViewportOverride)
				}
				return nil
			}, func() tea.Cmd {
				m.PruneTargetID = ""
				m.Notification = "Prune cancelled."
				return nil
			}))
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
				m.ViewMode = ModeChat
				if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().Name() == "map" {
					m.ViewStack.Pop()
				}
				return m, nil
			}
		}
	case "esc":
		m.ViewMode = ModeChat
		m.updateViewportContent()
		if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().Name() == "map" {
			m.ViewStack.Pop()
		}
		return m, nil
	}

	// Allow standard scrolling in map mode too
	var vCmd tea.Cmd
	m.Viewport, vCmd = m.Viewport.Update(msg)
	return m, vCmd
}

func (m *Model) handleMemoriesKeys(msg tea.KeyMsg) (*Model, tea.Cmd) {
	// 1. If viewing an expanded memory card detail
	if m.MemoryDetailCard != nil {
		switch msg.String() {
		case "esc", "backspace", "q":
			m.MemoryDetailCard = nil
			m.Viewport.SetContent(m.renderMemoriesView())
			return m, nil
		case "d", "x":
			card := m.MemoryDetailCard
			if m.Manager != nil && m.Manager.Storage != nil {
				if memStore, ok := m.Manager.Storage.(storage.MemoryStore); ok {
					_ = memStore.DeleteMemory(card.Scope, card.SessionID, card.Key)
				}
			}
			// Remove from deck if present
			if m.MemoryDeckIndex >= 0 && m.MemoryDeckIndex < len(m.MemoryDeck) {
				m.MemoryDeck = append(m.MemoryDeck[:m.MemoryDeckIndex], m.MemoryDeck[m.MemoryDeckIndex+1:]...)
				if m.MemoryDeckIndex >= len(m.MemoryDeck) {
					m.MemoryDeckIndex = len(m.MemoryDeck) - 1
				}
				if m.MemoryDeckIndex < 0 {
					m.MemoryDeckIndex = 0
				}
			}
			m.Notification = fmt.Sprintf("Pruned memory card %q", card.Key)
			m.MemoryDetailCard = nil
			m.Viewport.SetContent(m.renderMemoriesView())
			return m, nil
		}

		var vCmd tea.Cmd
		m.Viewport, vCmd = m.Viewport.Update(msg)
		return m, vCmd
	}

	// 2. Navigating the Card Deck
	if len(m.MemoryDeck) > 0 {
		nav := &ListNavigator{
			Cursor:   m.MemoryDeckIndex,
			Total:    len(m.MemoryDeck),
			PageSize: 5,
			Wrap:     true,
		}
		if nav.HandleKey(msg.String()) {
			m.MemoryDeckIndex = nav.Cursor
			m.Viewport.SetContent(m.renderMemoriesView())
			return m, nil
		}
	}

	switch msg.String() {
	case "enter", "space":
		if len(m.MemoryDeck) > 0 && m.MemoryDeckIndex >= 0 && m.MemoryDeckIndex < len(m.MemoryDeck) {
			m.MemoryDetailCard = &m.MemoryDeck[m.MemoryDeckIndex]
			m.Viewport.SetContent(m.renderMemoriesView())
			m.Viewport.GotoTop()
		}
		return m, nil
	case "d", "x":
		if len(m.MemoryDeck) > 0 && m.MemoryDeckIndex >= 0 && m.MemoryDeckIndex < len(m.MemoryDeck) {
			card := m.MemoryDeck[m.MemoryDeckIndex]
			if m.Manager != nil && m.Manager.Storage != nil {
				if memStore, ok := m.Manager.Storage.(storage.MemoryStore); ok {
					_ = memStore.DeleteMemory(card.Scope, card.SessionID, card.Key)
				}
			}
			m.MemoryDeck = append(m.MemoryDeck[:m.MemoryDeckIndex], m.MemoryDeck[m.MemoryDeckIndex+1:]...)
			if m.MemoryDeckIndex >= len(m.MemoryDeck) {
				m.MemoryDeckIndex = len(m.MemoryDeck) - 1
			}
			if m.MemoryDeckIndex < 0 {
				m.MemoryDeckIndex = 0
			}
			m.Notification = fmt.Sprintf("Pruned memory %q", card.Key)
			m.Viewport.SetContent(m.renderMemoriesView())
		}
		return m, nil
	case "esc", "q":
		m.ViewMode = ModeChat
		m.MemoryDetailCard = nil
		m.MemoryDeck = nil
		m.MemoryDeckFilter = ""
		m.updateViewportContent()
		if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().Name() == "memories" {
			m.ViewStack.Pop()
		}
		return m, nil
	}

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
		newNode, err := m.Manager.CreateNode("", domain.RoleSystem, input, false)
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
	// cancel the pending tools, pop the confirmation overlay, and proceed with the new message.
	if m.hasActiveOverlay("confirm_tool") {
		if m.ViewStack != nil && m.ViewStack.Top().Name() == "confirm_tool" {
			m.ViewStack.Pop()
		}
		for _, call := range m.PendingToolCalls {
			result := "Error: Tool call cancelled by user."
			_ = m.Manager.UpdateAssistantObservations(m.InterleavingNodeID, call.ID, result)
		}
		m.PendingToolCalls = nil
		m.Notification = "Pending tools cancelled."
	}

	// 3. Handle Regular Chat: Create a user node and trigger LLM generation.
	newNode, err := m.Manager.CreateNode(m.CurrentID, domain.RoleUser, input, false)
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
	if m.SessionID != "" && m.Manager != nil && m.Manager.Storage != nil {
		_ = m.Manager.Storage.SaveSessionHead(m.SessionID, m.CurrentID)
	}
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
		streamResponse(ctx, m.Provider, messages, m.Manager.Registry.GetToolSpecsForPolicy(m.Config.GetSandboxPolicy()), newNode.ID, ""),
		tick(),
	)
}
