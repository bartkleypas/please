package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
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

	ctxBadge := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("[Ctx: %s/%s %d%%]", usedStr, limitStr, pct))

	// Real-time persistent sandbox security badge
	policy := string(domain.SandboxPolicyStandard)
	if m.Config != nil {
		if pol := m.Config.GetSandboxPolicy(); pol != "" {
			policy = pol
		}
	}

	var sandboxBadge string
	switch strings.ToLower(policy) {
	case string(domain.SandboxPolicyStrict):
		sandboxBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#10b981")).Render("[🔒 STRICT]")
	case string(domain.SandboxPolicyPermissive):
		sandboxBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b")).Render("[⚠️ PERMISSIVE]")
	case string(domain.SandboxPolicyStandard):
		fallthrough
	default:
		sandboxBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4")).Render("[🛡️ STANDARD]")
	}

	right := sandboxBadge + " " + ctxBadge
	left := helpStyle.Render(leftHelp)

	if m.Width <= 0 {
		return left + "  " + right
	}

	gap := m.Width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
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

	if m.ViewStack != nil && m.ViewStack.Top() != nil {
		top := m.ViewStack.Top()
		if !top.IsOverlay() && top.Name() != "chat" && top.Name() != "map" && top.Name() != "memories" {
			return top.View(m.Width, m.Height)
		}
	}

	activeView := "chat"
	if m.ViewStack != nil && m.ViewStack.Top() != nil {
		activeView = m.ViewStack.Top().Name()
	}

	titleText := " PLEASE - Narrative Graph "
	if m.RemoteURL != "" {
		titleText = fmt.Sprintf(" PLEASE - Connected (%s) 🟢 ", m.RemoteURL)
	} else if activeView == "memories" || activeView == "memory_card" {
		titleText = " PLEASE - Memory Vault"
	}
	s := titleStyle.Render(titleText) + "\n\n"

	if m.Notification != "" {
		s += markStyle.Render("! "+m.Notification+" !") + "\n\n"
	}

	s += historyBoxStyle.Render(m.Viewport.View())

	if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().IsOverlay() {
		for _, layer := range m.ViewStack.layers {
			if aware, ok := layer.(interface{ setModel(*Model) }); ok {
				aware.setModel(&m)
			}
		}
		s += "\n" + m.ViewStack.Top().View(m.Width, m.Height)
	} else if m.IsCompressing {
		s += "\n" + botStyle.Render("Compressing narrative into Supernode...") + "\n"
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
	m.TextInput.Prompt = lipgloss.NewStyle().Foreground(color).Render("┃ ")
	m.TextInput.FocusedStyle.Prompt = lipgloss.NewStyle()

	// Footer Rendering
	switch activeView {
	case "map":
		if m.Searching {
			s += "\n\n" + inputBoxStyle.Render(m.SearchInput.View())
		} else if (m.ViewStack == nil || !m.ViewStack.Top().IsOverlay()) && !m.IsCompressing {
			s += "\n\n" + m.renderFooterHelp("h/l: fold/unfold • j/k: move • g/G: top/end • /: search • c: compact • d: prune • esc: chat")
		}
	case "memories", "memory_card":
		if m.MemoryDetailCard != nil {
			s += "\n\n" + m.renderFooterHelp("esc/backspace/q: back to deck • d/x: prune • ↑/↓: scroll")
		} else {
			s += "\n\n" + m.renderFooterHelp("↑/↓/j/k: select card • enter/space: flip card • d/x: prune • esc/q: chat")
		}
	default:
		if len(m.PendingImages) > 0 {
			var filenames []string
			for _, img := range m.PendingImages {
				filenames = append(filenames, filepath.Base(img))
			}
			s += "\n" + markStyle.Render(fmt.Sprintf("🖼️  Pending attachments: %s", strings.Join(filenames, ", "))) + "\n"
		}
		s += "\n\n" + inputBoxStyle.Render(m.TextInput.View())
		if m.hasActiveOverlay("confirm_tool") {
			s += "\n\n" + m.renderFooterHelp("(Press y/n to confirm/deny, or type a message to bypass)")
		} else {
			s += "\n\n" + m.renderFooterHelp("(/q to exit • /map for graph • /help for more)")
		}
	}

	return s
}
