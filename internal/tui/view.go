package tui

import (
	"fmt"
	"strings"

	"org.kleypas.please/internal/engine"
)

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
		s += markStyle.Render("! "+m.Notification+" !") + "\n\n"
	}

	s += historyBoxStyle.Render(m.Viewport.View())

	if m.AwaitingToolConfirmation {
		s += "\n" + markStyle.Render("Tool Call Confirmation Required:") + "\n"
		hasRedirection := false
		dangerChars := []string{">", "|", "&", "<"}

		for _, call := range m.PendingToolCalls {
			args := string(call.Function.Arguments)
			for _, char := range dangerChars {
				if strings.Contains(args, char) {
					hasRedirection = true
					break
				}
			}
			s += fmt.Sprintf(" - %s(%s)\n", call.Function.Name, args)
		}

		if hasRedirection {
			s += "\n" + warningStyle.Render("CAUTION: Shell redirection, piping, or chaining detected!") + "\n"
		}

		s += "\n" + markStyle.Render("Execute these tools? (y/n)") + "\n"
	} else if m.IsThinking {
		spinner := spinnerFrames[m.SpinnerFrame%len(spinnerFrames)]
		s += "\n" + botStyle.Render(fmt.Sprintf("%s Thinking...", spinner)) + "\n"
	}

	s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
	s += "\n\n(/q, /quit, or /bye to exit)" // Updated hint

	return s
}

// generateMapString builds the full visual tree of the graph
func (m *Model) generateMapString() string {
	var s strings.Builder
	s.WriteString("--- Narrative Graph Map ---\n\n")
	roots := m.Manager.GetRoots()
	if len(roots) == 0 {
		s.WriteString("No nodes found in graph.\n")
	} else {
		for i, root := range roots {
			isLast := i == len(roots)-1
			m.renderMap(&s, root.ID, "", isLast)
		}
	}
	return s.String()
}

func (m Model) renderMap(s *strings.Builder, nodeID string, indent string, isLast bool) {
	node, err := m.Manager.GetNode(nodeID)
	if err != nil {
		return
	}

	treePrefix := "•"
	if indent != "" {
		if isLast {
			treePrefix = "└"
		} else {
			treePrefix = "├"
		}
	}

	bookmark := ""
	if node.Metadata != nil && node.Metadata["bookmarked"] == "true" {
		bookmark = markStyle.Render("⭐")
	}

	// Tool call indicator
	toolIndicator := ""
	if len(node.ToolCalls) > 0 {
		toolIndicator = markStyle.Render("🛠️")
	} else if node.Role == engine.RoleTool {
		toolIndicator = markStyle.Render("⚙️")
	}

	// Current location marker moved here
	locationMarker := ""
	if node.ID == m.CurrentID {
		locationMarker = markStyle.Render("📍")
	}

	shortID := node.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	roleStyle := userStyle
	if node.Role == engine.RoleAssistant {
		roleStyle = botStyle
	} else if node.Role == engine.RoleTool {
		roleStyle = markStyle
	} else if node.Role == engine.RoleSystem {
		roleStyle = titleStyle
	}

	preview := node.Content
	if len(preview) > 30 {
		preview = preview[:27] + "..."
	}
	preview = strings.ReplaceAll(preview, "\n", " ")

	fmt.Fprintf(s, " %s%s[%s]%s%s%s %s:%s\n", indent, treePrefix, shortID, bookmark, toolIndicator, locationMarker, roleStyle.Render(string(node.Role)), preview)

	children := m.Manager.GetChildren(node.ID)
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
		m.renderMap(s, child.ID, childIndent, childIsLast)
	}
}
