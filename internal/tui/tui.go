package tui

import (
	"context"
	"fmt"
	"strings"

	"org.kleypas.please/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) renderNode(node *engine.Node) string {
	roleStyle := getRoleStyle(node.Role)

	prefix := string(node.Role)
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}

	content := node.Content
	if node.Role == engine.RoleAssistant && len(node.ToolCalls) > 0 {
		var toolStrings []string
		for _, tc := range node.ToolCalls {
			toolStrings = append(toolStrings, fmt.Sprintf("[Tool Call: %s(%s)]", tc.Function.Name, string(tc.Function.Arguments)))
		}
		if content != "" {
			content += "\n"
		}
		content += strings.Join(toolStrings, "\n")
	} else if node.Role == engine.RoleTool {
		content = fmt.Sprintf("[Tool Result (%s)]: %s", node.ToolCallID, content)
	}

	wrappedContent := wrapText(content, wrapWidth)
	return fmt.Sprintf("%s:\n%s\n", roleStyle.Render(prefix), wrappedContent)
}

func (m *Model) updateViewportWithNode(node *engine.Node) {
	line := m.renderNode(node)
	m.ChatHistoryBuffer += line
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
}

func (m *Model) updateViewportWithStreaming() {
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}
	wrappedContent := wrapText(m.CurrentStreamingContent, wrapWidth)

	line := fmt.Sprintf("%s:\n%s\n", botStyle.Render(string(engine.RoleAssistant)), wrappedContent)
	m.Viewport.SetContent(m.ChatHistoryBuffer + line)
	m.Viewport.GotoBottom()
}

func (m *Model) updateViewportWithJump() {
	m.updateViewportContent()
}

// Update is the main dispatcher for the Bubble Tea model. It delegates messages
// to specialized handler methods to maintain a flat and readable structure.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.handleTick()
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		return m.handleKeyEvent(msg)
	case llmStreamMsg:
		return m.handleLLMStream(msg)
	case llmStreamFinishedMsg:
		return m.handleLLMStreamFinished(msg)
	case streamResponseMsg:
		return m.handleStreamResponse(msg)
	case llmResponseMsg:
		return m.handleLLMResponse(msg)
	case exportResultMsg:
		return m.handleExportResult(msg)
	}

	return m, nil
}

func (m *Model) handleStreamResponse(msg streamResponseMsg) (tea.Model, tea.Cmd) {
	m.StreamContentChan = msg.contentChan
	m.StreamToolCallChan = msg.toolCallChan
	m.StreamErrChan = msg.errChan
	return m, waitForStream(m.StreamContentChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID)
}

// handleTick manages the spinner frame index and keeps the animation alive
// while the LLM is "thinking."
func (m *Model) handleTick() (tea.Model, tea.Cmd) {
	m.SpinnerFrame++
	if m.SpinnerFrame >= len(spinnerFrames) {
		m.SpinnerFrame = 0
	}
	if m.IsThinking {
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

	if m.AwaitingToolConfirmation {
		switch msg.String() {
		case "y", "Y":
			m.AwaitingToolConfirmation = false
			m.IsThinking = true
			return m, m.executeToolsCmd()
		case "n", "N":
			m.AwaitingToolConfirmation = false
			m.PendingToolCalls = nil
			m.Notification = "Tool calls cancelled by user."
			return m, nil
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

	input := m.TextInput.Value()
	if input == "" {
		return m, nil
	}

	// 1. Handle Setup Modes: Initial system prompt or new persona creation.
	if m.SetupMode || m.PersonaSetupMode {
		newNode, err := m.Manager.CreateNode("", engine.RoleSystem, input)
		if err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		m.CurrentID = newNode.ID
		m.SetupMode = false
		m.PersonaSetupMode = false
		m.updateViewportWithNode(newNode)
		m.TextInput.Reset()
		return m, nil
	}

	// 2. Handle Commands: Intercept and execute slash commands.
	if newM, cmd, handled := m.HandleCommand(input); handled {
		return newM, cmd
	}

	// 3. Handle Regular Chat: Create a user node and trigger LLM generation.
	newNode, err := m.Manager.CreateNode(m.CurrentID, engine.RoleUser, input)
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
		messages = append(messages, engine.Message{
			Role:    node.Role,
			Content: node.Content,
		})
	}

	// Trigger asynchronous generation.
	m.IsThinking = true
	return m, tea.Batch(
		streamResponse(m.Provider, messages, m.Manager.Registry.GetTools(), newNode.ID),
		tick(),
	)
}

// handleLLMStream appends incoming chunks to the streaming buffer and refreshes the view.
func (m *Model) handleLLMStream(msg llmStreamMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = false
	m.CurrentStreamingContent += msg.content
	m.updateViewportWithStreaming()
	return m, waitForStream(m.StreamContentChan, m.StreamToolCallChan, m.StreamErrChan, msg.parentID)
}

// handleLLMStreamFinished commits the full streamed response to the graph as a new node.
func (m *Model) handleLLMStreamFinished(msg llmStreamFinishedMsg) (tea.Model, tea.Cmd) {
	m.IsThinking = false
	if msg.err != nil {
		m.Notification = fmt.Sprintf("Error: %v", msg.err)
		m.CurrentStreamingContent = ""
		return m, nil
	}

	botNode, err := m.Manager.CreateAssistantNode(msg.parentID, m.CurrentStreamingContent, msg.toolCalls)
	if err != nil {
		m.Notification = fmt.Sprintf("Error: %v", err)
		m.CurrentStreamingContent = ""
		return m, nil
	}
	m.CurrentID = botNode.ID
	m.updateViewportContent() // Full refresh to show final formatted node
	m.CurrentStreamingContent = ""

	if len(msg.toolCalls) > 0 {
		m.PendingToolCalls = msg.toolCalls
		m.AwaitingToolConfirmation = true
	}

	return m, nil
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

func (m *Model) executeToolsCmd() tea.Cmd {
	return func() tea.Msg {
		var lastNodeID = m.CurrentID
		ctx := context.Background()

		for _, call := range m.PendingToolCalls {
			result, err := m.Manager.ExecuteToolCall(ctx, call)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			toolNode, err := m.Manager.CreateToolNode(lastNodeID, call.ID, result)
			if err != nil {
				return llmResponseMsg{err: fmt.Errorf("failed to create tool node: %w", err), parentID: lastNodeID}
			}
			lastNodeID = toolNode.ID
		}

		// After all tools executed, trigger a new LLM response (streaming)
		path, err := m.Manager.GetPath(lastNodeID)
		if err != nil {
			return llmResponseMsg{err: err, parentID: lastNodeID}
		}

		var messages []engine.Message
		for _, node := range path {
			messages = append(messages, engine.Message{
				Role:       node.Role,
				Content:    node.Content,
				ToolCalls:  node.ToolCalls,
				ToolCallID: node.ToolCallID,
			})
		}

		contentChan, toolCallChan, errChan := m.Provider.GenerateResponseStream(ctx, messages, m.Manager.Registry.GetTools())
		return streamResponseMsg{
			contentChan:  contentChan,
			toolCallChan: toolCallChan,
			errChan:      errChan,
			parentID:     lastNodeID,
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

	assistantNode, err := m.Manager.CreateAssistantNode(msg.parentID, msg.message.Content, msg.message.ToolCalls)
	if err != nil {
		m.Notification = fmt.Sprintf("Error: %v", err)
		return m, nil
	}

	m.CurrentID = assistantNode.ID
	m.updateViewportContent()

	if len(msg.message.ToolCalls) > 0 {
		m.PendingToolCalls = msg.message.ToolCalls
		m.AwaitingToolConfirmation = true
		return m, nil
	}

	return m, nil
}

func (m *Model) updateViewportContent() {
	m.ViewportOverride = "" // Clear override when refreshing chat history
	var s strings.Builder

	path, err := m.Manager.GetPath(m.CurrentID)
	if err != nil || len(path) == 0 {
		s.WriteString("Welcome to Please. Start typing to begin the story...\n")
	} else {
		for _, node := range path {
			s.WriteString(m.renderNode(node))
		}
	}

	m.ChatHistoryBuffer = s.String()
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
}
