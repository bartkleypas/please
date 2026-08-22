package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderFooterHelp(leftHelp string) string {
	used, limit, pct, color := m.ContextStats()
	usedStr := fmt.Sprintf("%.1fk", float64(used)/1000.0)
	if used < 1000 {
		usedStr = fmt.Sprintf("%d", used)
	}
	limitStr := fmt.Sprintf("%.0fk", float64(limit)/1000.0)
	if limit < 1000 {
		limitStr = fmt.Sprintf("%d", limit)
	}

	badge := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("[Ctx: %s/%s %d%%]", usedStr, limitStr, pct))
	left := helpStyle.Render(leftHelp)

	if m.Width <= 0 {
		return left + "  " + badge
	}

	gap := m.Width - lipgloss.Width(left) - lipgloss.Width(badge) - 2
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + badge
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

	titleText := " PLEASE - Narrative Graph "
	if m.RemoteURL != "" {
		titleText = fmt.Sprintf(" PLEASE - Connected (%s) 🟢 ", m.RemoteURL)
	}
	s := titleStyle.Render(titleText) + "\n\n"

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

	// Update dynamic color on the prompt block based on active context stats
	_, _, _, color := m.ContextStats()
	m.TextInput.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(color)

	// Footer Rendering
	if m.ViewMode == ModeMap {
		if m.Searching {
			s += "\n\n" + inputBoxStyle.Render(m.SearchInput.View())
		} else if !m.AwaitingPruneConfirmation && !m.AwaitingCompactConfirmation && !m.IsCompressing {
			s += "\n\n" + m.renderFooterHelp("h/l: fold/unfold • j/k: move • g/G: top/end • /: search • c: compact • d: prune • esc: chat")
		}
	} else {
		if len(m.PendingImages) > 0 {
			var filenames []string
			for _, img := range m.PendingImages {
				filenames = append(filenames, filepath.Base(img))
			}
			s += "\n" + markStyle.Render(fmt.Sprintf("🖼️  Pending attachments: %s", strings.Join(filenames, ", "))) + "\n"
		}
		s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
		if m.AwaitingToolConfirmation {
			s += "\n\n" + m.renderFooterHelp("(Press y/n to confirm/deny, or type a message to bypass)")
		} else if m.ViewportOverride != "" {
			s += "\n\n" + m.renderFooterHelp("(ESC: return to chat • ↑/↓ or PgUp/PgDn to scroll • /q to exit)")
		} else {
			s += "\n\n" + m.renderFooterHelp("(/q to exit • /map for graph • /help for more)")
		}
	}

	return s
}
