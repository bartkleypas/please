package tui

import (
	"fmt"
	"strings"
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

	if m.IsThinking {
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

	fmt.Fprintf(s, "%s%s[%s] %s: %s%s\n", indent, prefix, shortID, node.Role, preview, bookmark)

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
