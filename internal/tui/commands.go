package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
)

// Command defines the interface for TUI commands
type Command interface {
	Execute(m *Model, args []string) (tea.Model, tea.Cmd)
}

// commandRegistry maps command names to their implementation
var commandRegistry = make(map[string]Command)

func init() {
	commandRegistry["/jump"] = &JumpCommand{}
	commandRegistry["/list"] = &ListCommand{}
	commandRegistry["/mark"] = &MarkCommand{}
	commandRegistry["/unmark"] = &UnmarkCommand{}
	commandRegistry["/persona"] = &PersonaCommand{}
	commandRegistry["/map"] = &MapCommand{}
	commandRegistry["/config"] = &ConfigCommand{}
	commandRegistry["/sync"] = &SyncCommand{}
	commandRegistry["/server"] = &ServerCommand{}
	commandRegistry["/gc"] = &GCCommand{}
	commandRegistry["/emptytrash"] = &GCCommand{}
	commandRegistry["/help"] = &HelpCommand{}
	commandRegistry["/q"] = &QuitCommand{}
	commandRegistry["/quit"] = &QuitCommand{}
	commandRegistry["/bye"] = &QuitCommand{}
	commandRegistry["/audit"] = &AuditCommand{}
	commandRegistry["/version"] = &VersionCommand{}
	commandRegistry["/pacing"] = &PacingCommand{}
	commandRegistry["/bell"] = &BellCommand{}
	commandRegistry["/attach"] = &AttachCommand{}
	commandRegistry["/image"] = &AttachCommand{}
	commandRegistry["/fold"] = &FoldCommand{}
	commandRegistry["/compact"] = &CompactCommand{}
	commandRegistry["/compress"] = &CompactCommand{}
	commandRegistry["/session"] = &SessionCommand{}
	commandRegistry["/sessions"] = &SessionCommand{}
	commandRegistry["/worktree"] = &WorktreeCommand{}
	commandRegistry["/worktrees"] = &WorktreeCommand{}
	commandRegistry["/sandbox"] = &SandboxCommand{}
	commandRegistry["/memories"] = &MemoriesCommand{}
	commandRegistry["/memory"] = &MemoriesCommand{}

	// Tool confirmation commands
	commandRegistry["/yes"] = &ConfirmToolCommand{}
	commandRegistry["/confirm"] = &ConfirmToolCommand{}
	commandRegistry["/ok"] = &ConfirmToolCommand{}
	commandRegistry["/no"] = &CancelToolCommand{}
	commandRegistry["/cancel"] = &CancelToolCommand{}
	commandRegistry["/deny"] = &CancelToolCommand{}
}

// ... rest of HandleCommand ...

type AuditCommand struct{}

func (c *AuditCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.AuditMode = !m.AuditMode
	if m.AuditMode {
		m.Notification = "Audit Mode enabled: Internal nodes visible."
	} else {
		m.Notification = "Audit Mode disabled: Internal nodes hidden."
	}

	if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().Name() == "map" {
		m.Viewport.SetContent(m.generateMapString())
	} else {
		m.updateViewportContent()
	}

	return m, nil
}

type PacingCommand struct{}

func (c *PacingCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Config.Client == nil {
		m.Config.Client = &config.ClientConfig{}
	}
	if len(args) == 0 {
		pacing := !m.Config.EnableNaturalPacing()
		m.Config.Client.NaturalPacing = &pacing
	} else {
		switch strings.ToLower(args[0]) {
		case "on", "true", "yes":
			pacing := true
			m.Config.Client.NaturalPacing = &pacing
		case "off", "false", "no":
			pacing := false
			m.Config.Client.NaturalPacing = &pacing
		default:
			m.Notification = "Usage: /pacing [on|off]"
			return m, nil
		}
	}

	if m.Config.EnableNaturalPacing() {
		m.Notification = "Natural reading pacing enabled."
	} else {
		m.Notification = "Natural reading pacing disabled."
	}

	m.Config.RecordOrigin("client.natural_pacing", false)
	if !m.Config.ReadOnly {
		if savedPath, err := m.Config.SaveScoped(false); err == nil {
			m.Notification += fmt.Sprintf(" (saved to %s)", savedPath)
		}
	}
	return m, nil
}

type BellCommand struct{}

func (c *BellCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Config.Client == nil {
		m.Config.Client = &config.ClientConfig{}
	}
	if len(args) == 0 {
		bell := !m.Config.EnableBellOnTurnComplete()
		m.Config.Client.BellOnTurnComplete = &bell
	} else {
		switch strings.ToLower(args[0]) {
		case "on", "true", "yes":
			bell := true
			m.Config.Client.BellOnTurnComplete = &bell
		case "off", "false", "no":
			bell := false
			m.Config.Client.BellOnTurnComplete = &bell
		default:
			m.Notification = "Usage: /bell [on|off]"
			return m, nil
		}
	}

	if m.Config.EnableBellOnTurnComplete() {
		m.Notification = "🔔 Terminal bell enabled (ASCII 0x07 / \\a)"
	} else {
		m.Notification = "🔕 Terminal bell disabled"
	}

	m.Config.RecordOrigin("client.bell_on_turn_complete", false)
	if !m.Config.ReadOnly {
		if savedPath, err := m.Config.SaveScoped(false); err == nil {
			m.Notification += fmt.Sprintf(" (saved to %s)", savedPath)
		}
	}
	return m, nil
}

// HandleCommand checks if the input is a command and executes it.
// It returns (newModel, cmd, handled).
func (m *Model) HandleCommand(input string) (tea.Model, tea.Cmd, bool) {
	if !strings.HasPrefix(input, "/") {
		return m, nil, false
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return m, nil, false
	}

	commandName := parts[0]
	args := parts[1:]

	if cmdImpl, ok := commandRegistry[commandName]; ok {
		m.TextInput.Reset()
		newM, cmd := cmdImpl.Execute(m, args)
		return newM, cmd, true
	}

	m.Notification = "Unknown command: " + commandName
	m.TextInput.Reset()
	return m, nil, true
}

type SyncCommand struct{}

func (c *SyncCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.Notification = "Synchronizing with vault..."
	return m, syncVault(m.Manager)
}

type GCCommand struct{}

func (c *GCCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	count, err := m.Manager.GarbageCollect()
	if err != nil {
		m.Notification = fmt.Sprintf("GC failed: %v", err)
	} else {
		m.Notification = fmt.Sprintf("Garbage collection complete. %d nodes scrubbed.", count)
	}
	m.updateViewportContent()
	return m, nil
}

type ServerCommand struct{}

func (c *ServerCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Server == nil {
		m.Notification = "Error: Server not initialized"
		return m, nil
	}

	if len(args) == 0 || args[0] == "status" {
		running, port := m.Server.Status()
		if running {
			m.Notification = fmt.Sprintf("Server is running on http://localhost:%d", port)
		} else {
			m.Notification = "Server is stopped"
		}
		return m, nil
	}

	switch args[0] {
	case "on":
		port := 8080
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &port)
		}
		if err := m.Server.Start(port); err != nil {
			m.Notification = fmt.Sprintf("Error starting server: %v", err)
		} else {
			m.Notification = fmt.Sprintf("Server started on http://localhost:%d", port)
		}
	case "off":
		if err := m.Server.Stop(); err != nil {
			m.Notification = fmt.Sprintf("Error stopping server: %v", err)
		} else {
			m.Notification = "Server stopped"
		}
	default:
		m.Notification = "Usage: /server <on|off|status> [port]"
	}

	return m, nil
}

type HelpCommand struct{}

func (c *HelpCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	var s strings.Builder
	s.WriteString("--- 🦉 Please Help ---\n\n")
	s.WriteString("Interactive Commands:\n")
	s.WriteString("  /help           Show this help message\n")
	s.WriteString("  /map            Visualize the conversation graph\n")
	s.WriteString("  /list           List all node IDs in the current graph\n")
	s.WriteString("  /jump <id>      Jump to a specific node by its ID suffix\n")
	s.WriteString("  /mark [id]      Bookmark the current or specified node (suffix match)\n")
	s.WriteString("  /unmark <id>    Remove a bookmark from a node (suffix match)\n")
	s.WriteString("  /persona        Start a new timeline with a new system prompt\n")
	s.WriteString("  /config         View or update application settings\n")
	s.WriteString("  /sync           Reload the graph from disk to sync sessions\n")
	s.WriteString("  /gc             Permanently scrub soft-deleted nodes from disk\n")
	s.WriteString("  /server         Control the web visualization server (/server on|off|status)\n")
	s.WriteString("  /audit          Toggle full UUID visibility in the graph and chat views\n")
	s.WriteString("  /session [cmd]  Manage named sessions (/session [status], /session list, /session switch <name>)\n")
	s.WriteString("  /pacing         Toggle natural reading pacing for LLM stream (/pacing [on|off])\n")
	s.WriteString("  /bell           Toggle terminal bell cue on turn completion (/bell [on|off])\n")
	s.WriteString("  /compact [hint] Summarize the current branch into a milestone Supernode (alias: /compress)\n")
	s.WriteString("  /fold [all]     Fold/unfold reasoning thought process blocks (key: Tab / Shift+Tab)\n")
	s.WriteString("  /sandbox [mode] Inspect or set sandbox security policy (strict|standard|permissive)\n")
	s.WriteString("  /memories       Inspect persistent cybernetic agent memories (alias: /memory)\n")
	s.WriteString("  /parameters     Inspect Stable Diffusion metadata parameters for current node images (alias: /info)\n")
	s.WriteString("  /q, /quit, /bye Exit the application\n\n")
	s.WriteString("Navigation:\n")
	s.WriteString("  Use ↑/↓ or PgUp/PgDn to scroll through the conversation.\n")
	s.WriteString("  Press ESC to exit /map or /help views.\n")

	m.ensureViewStack()
	m.ViewStack.Push(NewTextViewLayer(m, "help", "Help", s.String(), ""))
	return m, nil
}

type QuitCommand struct{}

func (c *QuitCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	return m, tea.Quit
}

type VersionCommand struct{}

func (c *VersionCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.Notification = fmt.Sprintf("please version %s", engine.Version)
	return m, nil
}

type ConfirmToolCommand struct{}

func (c *ConfirmToolCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if !m.hasActiveOverlay("confirm_tool") && m.ActiveApprovalResp == nil {
		m.Notification = "No tool execution is pending confirmation."
		return m, nil
	}
	if m.hasActiveOverlay("confirm_tool") {
		m.ViewStack.Pop()
	}
	if m.ActiveApprovalResp != nil {
		m.ActiveApprovalResp <- true
		m.ActiveApprovalResp = nil
	}
	m.IsThinking = true
	return m, nil
}

type CancelToolCommand struct{}

func (c *CancelToolCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if !m.hasActiveOverlay("confirm_tool") && m.ActiveApprovalResp == nil {
		m.Notification = "No tool execution is pending confirmation."
		return m, nil
	}
	if m.hasActiveOverlay("confirm_tool") {
		m.ViewStack.Pop()
	}
	if m.ActiveApprovalResp != nil {
		m.ActiveApprovalResp <- false
		m.ActiveApprovalResp = nil
	}
	m.IsThinking = true
	return m, nil
}

type AttachCommand struct{}

func (c *AttachCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.Notification = "Usage: /attach <path>"
		return m, nil
	}
	path := strings.Trim(strings.Join(args, " "), `"'`)
	if _, err := os.Stat(path); err != nil {
		m.Notification = fmt.Sprintf("Error: file %s does not exist or is not readable", path)
		return m, nil
	}

	m.PendingImages = append(m.PendingImages, path)
	m.Notification = fmt.Sprintf("Attached image: %s", path)
	return m, nil
}

type FoldCommand struct{}

func (c *FoldCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.ExpandedThoughts == nil {
		m.ExpandedThoughts = make(map[string]bool)
	}

	if len(args) > 0 && args[0] == "all" {
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
		if newState {
			m.Notification = "Expanded all reasoning thoughts."
		} else {
			m.Notification = "Collapsed all reasoning thoughts."
		}
		return m, nil
	}

	// Toggle active turn
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
		if m.ExpandedThoughts[targetID] {
			m.Notification = fmt.Sprintf("Expanded thought process for %s", targetID)
		} else {
			m.Notification = fmt.Sprintf("Collapsed thought process for %s", targetID)
		}
	}
	return m, nil
}
