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

	if m.AwaitingPruneConfirmation {
		s += "\n" + warningStyle.Render("PRUNE BRANCH: This will hide this node and all descendants.") + "\n"
		s += markStyle.Render("Confirm pruning? (y/n)") + "\n"
	} else if m.AwaitingCompactConfirmation {
		s += "\n" + warningStyle.Render(fmt.Sprintf("COMPACT BRANCH: Summarize %d nodes into a Supernode?", len(m.CompactTargetIDs))) + "\n"
		s += markStyle.Render("Confirm compaction? (y/n)") + "\n"
	} else if m.IsCompressing {
		s += "\n" + botStyle.Render("Compressing narrative into Supernode...") + "\n"
	} else if m.AwaitingToolConfirmation {
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
		msg := "Thinking..."
		if m.InterleavingNodeID != "" {
			msg = "Executing tools..."
		}
		s += "\n" + botStyle.Render(fmt.Sprintf("%s %s", spinner, msg)) + "\n"
	}

	// Footer Rendering
	if m.ViewMode == ModeMap {
		if m.Searching {
			s += "\n\n" + inputBoxStyle.Render(m.SearchInput.View())
		} else if !m.AwaitingPruneConfirmation && !m.AwaitingCompactConfirmation && !m.IsCompressing {
			s += "\n\n" + helpStyle.Render("h/l: fold/unfold • j/k: move • g/G: top/end • /: search • c: compact • d: prune • esc: chat")
		}
	} else {
		s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
		if m.AwaitingToolConfirmation {
			s += "\n\n" + helpStyle.Render("(Press y/n to confirm/deny, or type a message/command to cancel & bypass them)")
		} else {
			s += "\n\n" + helpStyle.Render("(/q to exit • /map for graph • /help for more)")
		}
	}

	return s
}
