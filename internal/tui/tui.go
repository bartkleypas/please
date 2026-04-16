package tui

import (
	"context"
	"fmt"
	"strings"

	"org.kleypas.please/internal/engine"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) updateViewportWithNode(node *engine.Node) {
	role := userStyle
	if node.Role == engine.RoleAssistant {
		role = botStyle
	}
	prefix := string(node.Role)
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}
	wrappedContent := wrapText(node.Content, wrapWidth)

	line := fmt.Sprintf("%s:\n%s\n", role.Render(prefix), wrappedContent)
	m.ChatHistoryBuffer += line
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
}

func (m *Model) updateViewportWithStreaming() {
	role := botStyle
	prefix := string(engine.RoleAssistant)
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}
	wrappedContent := wrapText(m.CurrentStreamingContent, wrapWidth)

	line := fmt.Sprintf("%s:\n%s\n", role.Render(prefix), wrappedContent)
	m.Viewport.SetContent(m.ChatHistoryBuffer + line)
	m.Viewport.GotoBottom()
}

func (m *Model) updateViewportWithJump(path []*engine.Node) {
	var s_buf strings.Builder
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}
	for _, node := range path {
		role := userStyle
		if node.Role == engine.RoleAssistant {
			role = botStyle
		}
		prefix := string(node.Role)
		wrappedContent := wrapText(node.Content, wrapWidth)
		s_buf.WriteString(fmt.Sprintf("%s:\n%s\n", role.Render(prefix), wrappedContent))
	}
	m.ChatHistoryBuffer = s_buf.String()
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tickMsg:
		m.SpinnerFrame++
		if m.SpinnerFrame >= len(spinnerFrames) {
			m.SpinnerFrame = 0
		}
		if m.IsThinking {
			return m, tick()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Viewport.Width = msg.Width - 4    // Account for borders (2) and padding(2)
		m.Viewport.Height = msg.Height - 12 // Account for title, notification, input box, and borders
		m.updateViewportContent()
		return m, nil

	case tea.KeyMsg:

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
		case "ctrl+c": // Removed "q" to prevent accidental exits while typing
			return m, tea.Quit
		case "enter":
			if m.IsThinking {
				return m, nil
			}

			input := m.TextInput.Value()
			if input == "" {
				return m, nil
			}

			if m.SetupMode {
				newNode, err := m.Manager.CreateNode("", engine.RoleSystem, input)
				if err != nil {
					m.Notification = fmt.Sprintf("Error: %v", err)
					return m, nil
				}
				m.CurrentID = newNode.ID
				m.SetupMode = false
				m.updateViewportWithNode(newNode)
				m.TextInput.SetValue("")
				return m, nil
			}

			if m.PersonaSetupMode {
				newNode, err := m.Manager.CreateNode("", engine.RoleSystem, input)
				if err != nil {
					m.Notification = fmt.Sprintf("Error: %v", err)
					return m, nil
				}
				m.CurrentID = newNode.ID
				m.PersonaSetupMode = false
				m.updateViewportWithNode(newNode)
				m.TextInput.SetValue("")
				return m, nil
			}

			// Handle commands
			if strings.HasPrefix(input, "/") {
				parts := strings.Fields(input)
				commandName := parts[0]
				args := parts[1:]

				if cmd, ok := commandRegistry[commandName]; ok {
					return cmd.Execute(m, args)
				}

				m.Notification = "Unknown command: " + commandName
				m.TextInput.SetValue("")
				return m, nil
			}

			// 1. Create and save the user node
			newNode, err := m.Manager.CreateNode(m.CurrentID, engine.RoleUser, input)
			if err != nil {
				m.Notification = fmt.Sprintf("Error: %v", err)
				m.TextInput.SetValue("")
				return m, nil
			}
			m.CurrentID = newNode.ID
			m.updateViewportWithNode(newNode)

			m.TextInput.SetValue("")
			m.IsThinking = true

			// 2. Prepare context for LLM (the linear path)
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

			// 3. Trigger the asynchronous LLM call and the spinner
			m.CurrentStreamingContent = ""
			m.IsThinking = true
			ctx := context.Background() // TUI usually manages its own lifecycle
			m.StreamContentChan, m.StreamErrChan = m.Provider.GenerateResponseStream(ctx, messages)

			return m, tea.Batch(textinput.Blink, waitForStream(m.StreamContentChan, m.StreamErrChan, newNode.ID), tick())
		}

	case llmStreamMsg:
		m.IsThinking = false
		m.CurrentStreamingContent += msg.content
		m.updateViewportWithStreaming()
		return m, waitForStream(m.StreamContentChan, m.StreamErrChan, msg.parentID)

	case llmStreamFinishedMsg:
		m.IsThinking = false
		if msg.err != nil {
			m.Notification = fmt.Sprintf("Error: %v", msg.err)
			m.CurrentStreamingContent = ""
			return m, textinput.Blink
		}

		// Create and save the assistant node
		botNode, err := m.Manager.CreateNode(msg.parentID, engine.RoleAssistant, m.CurrentStreamingContent)
		if err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
			m.CurrentStreamingContent = ""
			return m, textinput.Blink
		}
		m.CurrentID = botNode.ID
		m.updateViewportWithNode(botNode)
		m.CurrentStreamingContent = ""

		return m, textinput.Blink

	case exportResultMsg:
		if msg.err != nil {
			m.Notification = fmt.Sprintf("Export failed: %v", msg.err)
		} else {
			m.Notification = fmt.Sprintf("Successfully exported to %s", msg.filename)
		}
		return m, nil
	}

	m.TextInput, cmd = m.TextInput.Update(msg)
	return m, cmd
}

func (m *Model) updateViewportContent() {
	m.ViewportOverride = "" // Clear override when refreshing chat history
	var s strings.Builder

	path, err := m.Manager.GetPath(m.CurrentID)
	if err != nil || len(path) == 0 {
		s.WriteString("Welcome to Please. Start typing to begin the story...\n")
	} else {
		for _, node := range path {
			role := userStyle
			if node.Role == engine.RoleAssistant {
				role = botStyle
			}

			prefix := string(node.Role)
			// Total overhead = Box borders/padding (4)
			wrapWidth := m.Width - 4
			wrappedContent := wrapText(node.Content, wrapWidth)
			s.WriteString(fmt.Sprintf("%s:\n%s\n", role.Render(prefix), wrappedContent))
		}
	}

	m.Viewport.SetContent(s.String())
	m.Viewport.GotoBottom()
}
