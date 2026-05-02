package tui

import (
	"context"
	"fmt"
	"time"

	"org.kleypas.please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleStreamResponse(streamMsg streamResponseMsg) (tea.Model, tea.Cmd) {
	m.StreamContentChan = streamMsg.contentChan
	m.StreamThoughtChan = streamMsg.thoughtChan
	m.StreamToolCallChan = streamMsg.toolCallChan
	m.StreamErrChan = streamMsg.errChan
	m.InterleavingNodeID = streamMsg.activeNodeID
	return m, tea.Batch(
		waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, streamMsg.parentID, streamMsg.activeNodeID),
		tick(),
	)
}

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

// handleTick manages the spinner frame index and keeps the animation alive
// while the LLM is "thinking" or while node creation animations are active.
func (m *Model) handleTick() (tea.Model, tea.Cmd) {
	m.SpinnerFrame++
	if m.SpinnerFrame >= len(spinnerFrames) {
		m.SpinnerFrame = 0
	}

	// Keep ticking if thinking OR if within the "pulse" animation window (5s after activity)
	if m.IsThinking || time.Since(m.LastActivity) < 5*time.Second {
		return m, tick()
	}
	return m, nil
}

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
	var cmd tea.Cmd

	if m.AwaitingCompactConfirmation {
		switch msg.String() {
		case "y", "Y":
			m.AwaitingCompactConfirmation = false
			m.IsCompressing = true
			return m, m.runCompaction()
		case "n", "N", "esc":
			m.AwaitingCompactConfirmation = false
			m.CompactTargetIDs = nil
			return m, nil
		}
		return m, nil
	}

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
			return m, nil
		case "n", "N", "esc":
			m.AwaitingPruneConfirmation = false
			m.PruneTargetID = ""
			return m, nil
		}
		return m, nil
	}

	if m.AwaitingToolConfirmation {
		switch msg.String() {
		case "y", "Y":
			m.AwaitingToolConfirmation = false
			m.IsThinking = true
			return m, m.executeToolsCmd()
		case "n", "N":
			m.AwaitingToolConfirmation = false
			m.IsThinking = true
			return m, m.cancelToolsCmd()
		}
		// If awaiting confirmation, ignore other keys for now or allow escape?
		if msg.String() == "esc" {
			m.AwaitingToolConfirmation = false
			m.PendingToolCalls = nil
			return m, nil
		}
		return m, nil
	}

	if m.ViewMode == ModeChat {
		// Only send specific scrolling keys to the viewport if we are typing,
		// or send all keys if the viewport override is active.
		if m.ViewportOverride != "" || msg.String() == "pgup" || msg.String() == "pgdown" || msg.String() == "up" || msg.String() == "down" {
			var vCmd tea.Cmd
			m.Viewport, vCmd = m.Viewport.Update(msg)
			cmd = tea.Batch(cmd, vCmd)
		}
	} else if m.ViewMode == ModeMap {
		if m.Searching {
			switch msg.String() {
			case "enter":
				m.SearchQuery = m.SearchInput.Value()
				m.Searching = false
				m.ViewportOverride = m.generateMapString()
				m.Viewport.SetContent(m.ViewportOverride)
				return m, nil
			case "esc":
				m.Searching = false
				m.SearchInput.Reset()
				m.SearchQuery = ""
				m.ViewportOverride = m.generateMapString()
				m.Viewport.SetContent(m.ViewportOverride)
				return m, nil
			}
			var siCmd tea.Cmd
			m.SearchInput, siCmd = m.SearchInput.Update(msg)
			return m, siCmd
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
			// Collapse or Ascend
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
		case "l":
			// Unfold or Descend
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
				m.CurrentID = m.MapNodeIDs[m.MapSelectionIndex]
				m.ViewMode = ModeChat
				m.ViewportOverride = ""
				m.updateViewportContent()
				return m, nil
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

	if m.ViewportOverride != "" {
		switch msg.String() {
		case "esc":
			m.ViewportOverride = ""
			m.updateViewportContent()
			return m, nil
		}
	}

	switch msg.String() {
	case "ctrl+c":
		if m.StreamCancel != nil {
			m.StreamCancel()
		}
		return m, tea.Quit
	case "enter":
		return m.handleEnterKey()
	}

	var tiCmd tea.Cmd
	m.TextInput, tiCmd = m.TextInput.Update(msg)
	return m, tea.Batch(cmd, tiCmd)
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

	// 3. Handle Regular Chat: Create a user node and trigger LLM generation.
	newNode, err := m.Manager.CreateNode(m.CurrentID, engine.RoleUser, input, false)
	if err != nil {
		m.Notification = fmt.Sprintf("Error: %v", err)
		m.TextInput.Reset()
		return m, nil
	}
	m.CurrentID = newNode.ID
	m.updateViewportWithNode(newNode)

	m.TextInput.Reset()
	m.IsThinking = true

	// Prepare context for LLM by fetching the linear path from the DAG root.
	path, err := m.Manager.GetPath(m.CurrentID)
	if err != nil {
		m.IsThinking = false
		return m, nil
	}

	var messages []engine.Message
	for _, node := range path {
		msg := engine.Message{
			Role:       node.Role,
			Content:    node.Content,
			ToolCallID: node.ToolCallID,
			Internal:   node.Internal,
		}

		// Only include reasoning/observations for the active node (if we were resuming, but here we are starting a new turn)
		// Since this is a new turn, all previous assistant nodes are historical.
		if node.Role != engine.RoleAssistant {
			msg.Thought = node.Thought
			msg.ToolCalls = node.ToolCalls
			msg.Observations = node.Observations
		}

		messages = append(messages, msg)
	}

	// Trigger asynchronous generation.
	m.IsThinking = true
	ctx, cancel := context.WithCancel(context.Background())
	m.StreamCancel = cancel

	return m, tea.Batch(
		streamResponse(ctx, m.Provider, messages, m.Manager.Registry.GetTools(), newNode.ID, ""),
		tick(),
	)
}

// handleLLMStream appends incoming chunks to the streaming buffer and refreshes the view.
func (m *Model) handleLLMStream(msg llmStreamMsg) (tea.Model, tea.Cmd) {
	// Keep IsThinking true while streaming to maintain the spinner/animation
	m.IsThinking = true
	m.CurrentStreamingContent += msg.content
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID, msg.activeNodeID)
}

// handleLLMThoughtStream appends incoming reasoning chunks to the streaming thought buffer.
func (m *Model) handleLLMThoughtStream(msg llmThoughtStreamMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = true
	m.CurrentStreamingThought += msg.thought
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamThoughtChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID, msg.activeNodeID)
}

// handleLLMStreamFinished commits the full streamed response to the graph as a new node or updates existing.
func (m *Model) handleLLMStreamFinished(msg llmStreamFinishedMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = false
	if m.StreamCancel != nil {
		m.StreamCancel()
		m.StreamCancel = nil
	}

	if msg.err != nil {
		m.Notification = fmt.Sprintf("Error: %v", msg.err)
		m.CurrentStreamingContent = ""
		m.CurrentStreamingThought = ""
		return m, nil
	}

	var activeID string
	if msg.activeNodeID != "" {
		// Update existing node
		node, err := m.Manager.GetNode(msg.activeNodeID)
		if err != nil {
			m.Notification = fmt.Sprintf("Error finding node: %v", err)
			return m, nil
		}
		node.Content += m.CurrentStreamingContent
		node.Thought += m.CurrentStreamingThought
		node.ToolCalls = append(node.ToolCalls, msg.toolCalls...)

		// Re-persist existing node state (SaveNode is now INSERT OR REPLACE)
		if err := m.Manager.Storage.SaveNode(node); err != nil {
			m.Notification = fmt.Sprintf("Error saving node: %v", err)
		}
		activeID = msg.activeNodeID
	} else {
		// Create assistant node as child of the designated parent (preserving continuity)
		botNode, err := m.Manager.CreateAssistantNode(msg.parentID, m.CurrentStreamingContent, m.CurrentStreamingThought, msg.toolCalls, false)
		if err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
			m.CurrentStreamingContent = ""
			m.CurrentStreamingThought = ""
			return m, nil
		}
		activeID = botNode.ID
	}

	m.CurrentID = activeID
	m.LastActivity = time.Now()
	m.updateViewportContent() // Full refresh to show final formatted node
	m.CurrentStreamingContent = ""
	m.CurrentStreamingThought = ""
	m.InterleavingNodeID = ""

	if len(msg.toolCalls) > 0 {
		m.PendingToolCalls = msg.toolCalls
		m.InterleavingNodeID = activeID // Mark this node for interleaving

		// Check if all tool calls are non-interactive
		allNonInteractive := true
		for _, call := range msg.toolCalls {
			if tool, ok := m.Manager.Registry.Tools[call.Function.Name]; ok {
				if tool.Interactive {
					allNonInteractive = false
					break
				}
			} else {
				// Unknown tools default to interactive for safety
				allNonInteractive = false
				break
			}
		}

		if allNonInteractive {
			m.IsThinking = true
			return m, m.executeToolsCmd()
		}

		m.AwaitingToolConfirmation = true
	}

	return m, tea.Batch(tick())
}

// handleExportResult provides visual feedback for data export operations.
func (m *Model) handleExportResult(msg exportResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Export failed: %v", msg.err)
	} else {
		m.Notification = fmt.Sprintf("Successfully exported to %s", msg.filename)
	}
	return m, nil
}

// handleSyncResult refreshes the view after a manual synchronization.
func (m *Model) handleSyncResult(msg syncResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Sync failed: %v", msg.err)
	} else {
		m.Notification = "Graph synchronized with disk."
		m.updateViewportContent()
	}
	return m, nil
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

func (m *Model) resumeStreamCmd(ctx context.Context, activeNodeID string) tea.Cmd {
	return func() tea.Msg {
		path, err := m.Manager.GetPath(activeNodeID)
		if err != nil {
			return llmResponseMsg{err: err, parentID: activeNodeID}
		}

		var messages []engine.Message
		for _, node := range path {
			msg := engine.Message{
				Role:       node.Role,
				Content:    node.Content,
				ToolCallID: node.ToolCallID,
				Internal:   node.Internal,
			}

			// For Assistant nodes, only keep Thought/Tools/Observations if it's the active node we are resuming.
			// This provides continuity for the current turn while saving tokens on previous ones.
			if node.Role != engine.RoleAssistant || node.ID == activeNodeID {
				msg.Thought = node.Thought
				msg.ToolCalls = node.ToolCalls
				msg.Observations = node.Observations
			}

			messages = append(messages, msg)
		}

		contentChan, thoughtChan, toolCallChan, errChan := m.Provider.GenerateResponseStream(ctx, messages, m.Manager.Registry.GetTools())
		return streamResponseMsg{
			contentChan:  contentChan,
			thoughtChan:  thoughtChan,
			toolCallChan: toolCallChan,
			errChan:      errChan,
			parentID:     "", // Parent is irrelevant during interleaving resumption
			activeNodeID: activeNodeID,
		}
	}
}

// handleLLMResponse handles non-streaming LLM responses, potentially containing tool calls.
func (m *Model) handleLLMResponse(msg llmResponseMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = false
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Error: %v", msg.err)
		return m, nil
	}

	assistantNode, err := m.Manager.CreateAssistantNode(msg.parentID, msg.message.Content, msg.message.Thought, msg.message.ToolCalls, false)
	if err != nil {
		m.Notification = fmt.Sprintf("Error: %v", err)
		return m, nil
	}

	m.CurrentID = assistantNode.ID
	m.LastActivity = time.Now()
	m.updateViewportContent()

	if len(msg.message.ToolCalls) > 0 {
		m.PendingToolCalls = msg.message.ToolCalls
		m.AwaitingToolConfirmation = true
		return m, nil
	}

	return m, tea.Batch(tick())
}

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
