package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"org.kleypas.please/internal/engine"
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

	switch m.ViewMode {
	case ModeChat:
		m.updateViewportContent()
	case ModeMap:
		m.ViewportOverride = m.generateMapString()
		m.Viewport.SetContent(m.ViewportOverride)
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

type JumpCommand struct{}

func (c *JumpCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /jump <id_prefix>"
		return m, nil
	}

	prefix := args[0]
	if node, err := m.Manager.FindNodeByShortID(prefix); err == nil {
		m.CurrentID = node.ID
		m.Notification = fmt.Sprintf("Jumped to %s", node.ID)
		m.updateViewportWithJump()
		return m, nil
	}

	m.Notification = fmt.Sprintf("Error: No node matching %s", prefix)
	return m, nil
}

type ListCommand struct{}

func (c *ListCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	nodes := m.Manager.GetAllNodeIDs()
	if len(nodes) == 0 {
		m.ViewportOverride = "No nodes found in graph."
	} else {
		m.ViewportOverride = "--- Node List ---\n" + strings.Join(nodes, "\n")
	}
	m.Viewport.SetContent(m.ViewportOverride)
	m.Viewport.GotoTop()
	return m, nil
}

type MarkCommand struct{}

func (c *MarkCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	targetID := m.CurrentID
	if len(args) > 0 {
		targetID = args[0]
	}

	if node, err := m.Manager.GetNode(targetID); err == nil {
		if err := m.Manager.SetBookmark(node.ID, true); err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
		} else {
			m.Notification = fmt.Sprintf("Node %s bookmarked!", targetID)
		}
	} else {
		m.Notification = "Error: Node not found"
	}
	return m, nil
}

type UnmarkCommand struct{}

func (c *UnmarkCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /unmark <id>"
		return m, nil
	}

	targetID := args[0]
	if node, err := m.Manager.GetNode(targetID); err == nil {
		if err := m.Manager.SetBookmark(node.ID, false); err != nil {
			m.Notification = fmt.Sprintf("Error: %v", err)
		} else {
			m.Notification = "Node " + targetID + " unbookmarked!"
		}
	} else {
		m.Notification = "Error: Node not found"
	}
	return m, nil
}

type PersonaCommand struct{}

func (c *PersonaCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.PersonaSetupMode = true
	return m, nil
}

type MapCommand struct{}

func (c *MapCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.ViewMode = ModeMap
	m.MapSelectionIndex = 0
	m.ViewportOverride = m.generateMapString()
	m.Viewport.SetContent(m.ViewportOverride)
	m.Viewport.GotoTop()
	return m, nil
}

type ConfigCommand struct{}

func (c *ConfigCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		var s strings.Builder
		s.WriteString("--- ⚙️ Current Configuration ---\n\n")
		s.WriteString(fmt.Sprintf("  Model:    %s\n", m.Config.Model))
		s.WriteString(fmt.Sprintf("  Endpoint: %s\n", m.Config.Endpoint))
		s.WriteString(fmt.Sprintf("  Vault:    %s\n", m.Config.VaultPath))
		s.WriteString("\nUsage:\n")
		s.WriteString("  /config model <name>      Change the LLM model\n")
		s.WriteString("  /config endpoint <url>   Change the API endpoint\n")

		m.ViewportOverride = s.String()
		m.Viewport.SetContent(m.ViewportOverride)
		m.Viewport.GotoTop()
		return m, nil
	}

	if len(args) < 2 {
		m.Notification = "Usage: /config <model|endpoint> <value>"
		return m, nil
	}

	key := args[0]
	value := args[1]

	switch key {
	case "model":
		m.Config.Model = value
		if op, ok := m.Provider.(*engine.OllamaProvider); ok {
			op.Model = value
		}
		m.Notification = "Model updated to " + value
	case "endpoint":
		m.Config.Endpoint = value
		if op, ok := m.Provider.(*engine.OllamaProvider); ok {
			op.Endpoint = value
		}
		m.Notification = "Endpoint updated to " + value
	default:
		m.Notification = "Unknown config key: " + key
		return m, nil
	}

	if err := m.Config.Save(); err != nil {
		m.Notification = fmt.Sprintf("Error saving config: %v", err)
	}

	return m, nil
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
	s.WriteString("  /q, /quit, /bye Exit the application\n\n")
	s.WriteString("Navigation:\n")
	s.WriteString("  Use ↑/↓ or PgUp/PgDn to scroll through the conversation.\n")
	s.WriteString("  Press ESC to exit /map or /help views.\n")

	m.ViewportOverride = s.String()
	m.Viewport.SetContent(m.ViewportOverride)
	m.Viewport.GotoTop()
	return m, nil
}

type QuitCommand struct{}

func (c *QuitCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	return m, tea.Quit
}
