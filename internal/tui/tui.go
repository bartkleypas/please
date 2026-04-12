package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"org.kleypas.please/internal/engine"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// tickMsg is sent to trigger the spinner animation
type tickMsg struct{}

// llmResponseMsg is sent when the LLM provider returns a result
type llmResponseMsg struct {
	content  string
	err      error
	parentID string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇"}

// exportResultMsg is sent when the export file operation completes
type exportResultMsg struct {
	filename string
	err      error
}

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Background(lipgloss.Color("#7D56F4")).Padding(0, 1)
	userStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	botStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF"))
	markStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFCC00"))
	historyBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)
)

// wrapText is a helper to manually insert newlines into long strings
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	var wrapped strings.Builder

	for i, line := range lines {
		if line == "" {
			wrapped.WriteString("\n")
			continue
		}

		// 1. Capture leading whitespace
		trimmed := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(trimmed)]

		// 2. Wrap the trimmed content
		words := strings.Fields(trimmed)
		var lineBuilder strings.Builder
		currentLineLength := 0

		for _, word := range words {
			wordLen := len(word)
			if currentLineLength+wordLen+1 > width {
				lineBuilder.WriteString("\n")
				currentLineLength = 0
			} else if currentLineLength > 0 {
				lineBuilder.WriteString(" ")
				currentLineLength++
			}
			lineBuilder.WriteString(word)
			currentLineLength += wordLen
		}

		// 3. Prepend the original indentation to the first line of the wrapped result
		wrapped.WriteString(indent + lineBuilder.String())

		if i < len(lines)-1 {
			wrapped.WriteString("\n")
		}
	}

	return wrapped.String()
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// callLLM is a Bubble Tea command that wraps the provider's GenerateResponse
func callLLM(provider engine.LLMProvider, messages []engine.Message, parentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		content, err := provider.GenerateResponse(ctx, messages)
		return llmResponseMsg{
			content:  content,
			err:      err,
			parentID: parentID,
		}
	}
}

// tick is a command that sends a tickMsg after a short delay
func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

// generateMapString builds the full visual tree of the graph
func (m Model) generateMapString() string {
	var s strings.Builder
	s.WriteString("--- Narrative Graph Map ---\n\n")
	roots := m.Graph.GetRoots()
	if len(roots) == 0 {
		s.WriteString("No nodes found in graph.\n")
	} else {
		for i, root := range roots {
			isLast := i == len(roots)-1
			s.WriteString(m.renderMap(root.ID, "", isLast))
		}
	}
	return s.String()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			var vCmd tea.Cmd
			m.Viewport, vCmd = m.Viewport.Update(msg)
			cmd = tea.Batch(cmd, vCmd)
		}

		if m.ViewportOverride != "" {
			switch msg.String() {
			case "esc", "backspace":
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
				newNode := &engine.Node{
					ID:        uuid.NewString(),
					ParentID:  "",
					Role:      engine.RoleSystem,
					Content:   input,
					Timestamp: time.Now(),
				}
				m.Graph.AddNode(newNode)
				m.Storage.SaveNode(newNode)
				m.CurrentID = newNode.ID
				m.SetupMode = false
				m.updateViewportContent()
				m.TextInput.SetValue("")
				return m, nil
			}

			if m.PersonaSetupMode {
				newNode := &engine.Node{
					ID:        uuid.NewString(),
					ParentID:  "",
					Role:      engine.RoleSystem,
					Content:   input,
					Timestamp: time.Now(),
				}
				m.Graph.AddNode(newNode)
				m.Storage.SaveNode(newNode)
				m.CurrentID = newNode.ID
				m.PersonaSetupMode = false
				m.updateViewportContent()
				m.TextInput.SetValue("")
				return m, nil
			}

			if input == "" {
				return m, nil
			}
			if input == "" {
				return m, nil
			}

			// Handle commands
			if strings.HasPrefix(input, "/") {
				parts := strings.Fields(input)
				command := parts[0]

				switch command {
				case "/jump":
					if len(parts) == 2 {
						prefix := parts[1]
						if node, err := m.Graph.FindNodeByPrefix(prefix); err == nil {
							m.CurrentID = node.ID
							m.Notification = fmt.Sprintf("Jumped to %s", node.ID)
							m.TextInput.SetValue("")
							m.updateViewportContent()
							return m, nil
						}
						m.Notification = fmt.Sprintf("Error: No node matching %s", prefix)
					} else {
						m.Notification = "Usage: /jump <id_prefix>"
					}
					m.TextInput.SetValue("")
					return m, nil

				case "/list":
					var nodes []string
					for id := range m.Graph.Nodes {
						nodes = append(nodes, id)
					}
					if len(nodes) == 0 {
						m.ViewportOverride = "No nodes found in graph."
					} else {
						m.ViewportOverride = "--- Node List ---\n" + strings.Join(nodes, "\n")
					}
					m.Viewport.SetContent(m.ViewportOverride)
					m.Viewport.GotoBottom()
					m.TextInput.SetValue("")
					return m, nil

				case "/mark":
					targetID := m.CurrentID
					if len(parts) == 2 {
						targetID = parts[1]
					}

					if node, err := m.Graph.GetNode(targetID); err == nil {
						if node.Metadata == nil {
							node.Metadata = make(map[string]string)
						}
						node.Metadata["bookmarked"] = "true"
						m.Notification = fmt.Sprintf("Node %s bookmarked!", targetID)
					} else {
						m.Notification = "Error: Node not found"
					}
					m.TextInput.SetValue("")
					return m, nil

				case "/unmark":
					if len(parts) == 2 {
						targetID := parts[1]
						if node, err := m.Graph.GetNode(targetID); err == nil {
							if node.Metadata != nil {
								delete(node.Metadata, "bookmarked")
								m.Notification = "Node " + targetID + " unbookmarked!"
							} else {
								m.Notification = "Node has no metadata to unmark."
							}
						} else {
							m.Notification = "Error: Node not found"
						}
					} else {
						m.Notification = "Usage: /unmark <id>"
					}
					m.TextInput.SetValue("")
					return m, nil

				case "/persona":
					m.PersonaSetupMode = true
					m.TextInput.SetValue("")
					return m, nil

				case "/map":
					m.ViewportOverride = m.generateMapString()
					m.Viewport.SetContent(m.ViewportOverride)
					m.Viewport.GotoBottom()
					m.TextInput.SetValue("")
					return m, nil

				case "/q", "/quit", "/bye":
					return m, tea.Quit

				default:
					m.Notification = "Unknown command: " + command
					m.TextInput.SetValue("")
					return m, nil
				}
			}

			// 1. Create and save the user node
			newNode := &engine.Node{
				ID:        uuid.NewString(),
				ParentID:  m.CurrentID,
				Role:      engine.RoleUser,
				Content:   input,
				Timestamp: time.Now(),
			}
			m.Graph.AddNode(newNode)
			m.Storage.SaveNode(newNode)
			m.CurrentID = newNode.ID
			m.updateViewportContent()

			m.TextInput.SetValue("")
			m.IsThinking = true

			// 2. Prepare context for LLM (the linear path)
			path, err := m.Graph.GetPath(m.CurrentID)
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
			return m, tea.Batch(textinput.Blink, callLLM(m.Provider, messages, newNode.ID), tick())
		}

	case llmResponseMsg:
		m.IsThinking = false
		if msg.err != nil {
			m.Notification = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}

		// Create and save the assistant node
		botNode := &engine.Node{
			ID:        uuid.NewString(),
			ParentID:  msg.parentID,
			Role:      engine.RoleAssistant,
			Content:   msg.content,
			Timestamp: time.Now(),
		}
		m.Graph.AddNode(botNode)
		m.Storage.SaveNode(botNode)
		m.CurrentID = botNode.ID
		m.updateViewportContent()

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

	path, err := m.Graph.GetPath(m.CurrentID)
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

func (m Model) View() string {
	if m.PersonaSetupMode {
		s := titleStyle.Render(" PLEASE - New Persona ") + "\n\n"
		s += "Define a new system prompt to switch personas.\n"
		s += "This will create a new root node and jump you to it.\n"
		s += "Example: 'You are a grumpy old librarian.'\n\n"
		s += "Press Enter to initialize the new persona.\n\n"
		s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
		return s
	}

	if m.SetupMode {
		s := titleStyle.Render(" PLEASE - Setup ") + "\n\n"
		s += "Welcome to Please.\n\n"
		s += "Please define the System Prompt (the rules of this universe) to begin.\n"
		s += "Example: 'You are a helpful assistant who speaks like a pirate.'\n\n"
		s += "Press Enter to initialize the graph.\n\n"
		s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
		return s
	}

	s := titleStyle.Render(" PLEASE - Narrative Graph ") + "\n\n"

	if m.Notification != "" {
		s += lipgloss.NewStyle().Foreground(lipgloss.Color("#FFCC00")).Render("! "+m.Notification+" !") + "\n\n"
	}

	s += historyBoxStyle.Render(m.Viewport.View())

	if m.IsThinking {
		spinner := spinnerFrames[m.SpinnerFrame%len(spinnerFrames)]
		s += "\n" + botStyle.Render(fmt.Sprintf("%s Thinking...", spinner)) + "\n"
	}

	s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
	s += "\n\n(/q, /quit, or /bye to exit)" // Updated hint

	return s
}

func (m Model) renderMap(nodeID string, indent string, isLast bool) string {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		return ""
	}

	prefix := "•"
	if indent != "" {
		if isLast {
			prefix = "└"
		} else {
			prefix = "├"
		}
	}

	bookmark := ""
	if node.Metadata != nil && node.Metadata["bookmarked"] == "true" {
		bookmark = markStyle.Render(" ⭐")
	}

	shortID := node.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	preview := node.Content
	if len(preview) > 40 {
		preview = preview[:37] + "..."
	}
	preview = strings.ReplaceAll(preview, "\n", " ")

	res := fmt.Sprintf("%s%s[%s] %s: %s%s\n", indent, prefix, shortID, node.Role, preview, bookmark)

	children := m.Graph.GetChildren(node.ID)
	for i, child := range children {
		childIsLast := i == len(children)-1
		childIndent := indent
		if indent == "" {
			childIndent = " "
		} else if isLast {
			childIndent += " "
		} else {
			childIndent += "│"
		}
		res += m.renderMap(child.ID, childIndent, childIsLast)
	}

	return res
}
