package tui

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

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
	commandRegistry["/attach"] = &AttachCommand{}
	commandRegistry["/image"] = &AttachCommand{}
	commandRegistry["/parameters"] = &ParametersCommand{}
	commandRegistry["/info"] = &ParametersCommand{}
	commandRegistry["/fold"] = &FoldCommand{}
	commandRegistry["/compact"] = &CompactCommand{}
	commandRegistry["/compress"] = &CompactCommand{}

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

	switch m.ViewMode {
	case ModeChat:
		m.updateViewportContent()
	case ModeMap:
		m.ViewportOverride = m.generateMapString()
		m.Viewport.SetContent(m.ViewportOverride)
	}

	return m, nil
}

type PacingCommand struct{}

func (c *PacingCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Config.Client == nil {
		m.Config.Client = &engine.ClientConfig{}
	}
	if len(args) == 0 {
		pacing := !m.Config.IsPacingEnabled()
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

	if m.Config.IsPacingEnabled() {
		m.Notification = "Natural reading pacing enabled."
	} else {
		m.Notification = "Natural reading pacing disabled."
	}

	_ = m.Config.Save()
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
		m.navigateToNode(node)
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

type CompactCommand struct{}

func (c *CompactCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	rangeIDs := m.getCompactionRange(m.CurrentID)
	if len(rangeIDs) == 0 {
		m.Notification = "Nothing to compact on current branch."
		return m, nil
	}

	directive := strings.Join(args, " ")
	m.CompactTargetIDs = rangeIDs
	m.CompactDirective = directive
	m.IsCompressing = true
	m.Notification = "Compacting branch into Supernode..."
	return m, m.runCompaction()
}

func (m *Model) syncProviderOptions() {
	if m.Config.Server == nil {
		return
	}
	if op, ok := m.Provider.(*engine.OllamaProvider); ok {
		op.Options = m.Config.Server.Options
	} else if op, ok := m.Provider.(*engine.OpenAIProvider); ok {
		op.Options = m.Config.Server.Options
	}
}

func (m *Model) renderConfigString() string {
	var s strings.Builder
	s.WriteString("--- ⚙️  Configuration & Engine State ---\n\n")

	srv := m.Config.Server
	if srv == nil {
		srv = &engine.ServerConfig{}
	}
	cli := m.Config.Client
	if cli == nil {
		cli = &engine.ClientConfig{}
	}

	// Active Session
	sessionStr := "Standalone (Local Embedded Engine)"
	if m.RemoteURL != "" {
		sessionStr = fmt.Sprintf("Connected (%s 🟢)", m.RemoteURL)
	}
	fmt.Fprintf(&s, "  • Active Session:  %s\n\n", sessionStr)

	// [ Server / Engine Backend ]
	s.WriteString("  [ Server / Engine Backend ]\n")
	providerStr := srv.Provider
	if providerStr == "" {
		providerStr = "ollama"
	}
	fmt.Fprintf(&s, "    Provider:        %s (%s)\n", providerStr, srv.Model)
	fmt.Fprintf(&s, "    Endpoint:        %s\n", srv.Endpoint)
	storageType := srv.StorageType
	if storageType == "" {
		storageType = "sqlite"
	}
	fmt.Fprintf(&s, "    Vault:           %s (%s)\n", srv.VaultPath, storageType)
	wsStr := srv.WorkspaceDir
	if wsStr == "" {
		wsStr = "(current directory)"
	}
	fmt.Fprintf(&s, "    Workspace:       %s\n", wsStr)
	encStr := "(disabled)"
	if srv.EncryptionKey != "" {
		encStr = "•••••••• (configured)"
	}
	fmt.Fprintf(&s, "    Encryption:      %s\n", encStr)
	authStr := "disabled (open local)"
	if srv.AuthToken != "" {
		authStr = "enabled (Bearer token active)"
	}
	fmt.Fprintf(&s, "    Authentication:  %s\n", authStr)

	s.WriteString("\n    Inference Parameters:\n")
	if srv.Options != nil {
		if srv.Options.Temperature != nil {
			fmt.Fprintf(&s, "      Temperature:   %.2f\n", *srv.Options.Temperature)
		} else {
			s.WriteString("      Temperature:   (default)\n")
		}
		if srv.Options.TopP != nil {
			fmt.Fprintf(&s, "      Top P:         %.2f\n", *srv.Options.TopP)
		} else {
			s.WriteString("      Top P:         (default)\n")
		}
		if srv.Options.TopK != nil {
			fmt.Fprintf(&s, "      Top K:         %d\n", *srv.Options.TopK)
		} else {
			s.WriteString("      Top K:         (default)\n")
		}
		if srv.Options.NumCtx != nil {
			fmt.Fprintf(&s, "      Context Size:  %d tokens\n", *srv.Options.NumCtx)
		} else {
			s.WriteString("      Context Size:  (default)\n")
		}
		if srv.Options.MaxTokens != nil {
			fmt.Fprintf(&s, "      Max Tokens:    %d tokens\n", *srv.Options.MaxTokens)
		} else {
			s.WriteString("      Max Tokens:    (default)\n")
		}
		if srv.Options.RepeatPenalty != nil {
			fmt.Fprintf(&s, "      Repeat Penalty:%.2f\n", *srv.Options.RepeatPenalty)
		} else {
			s.WriteString("      Repeat Penalty:(default)\n")
		}
	} else {
		s.WriteString("      (All provider defaults)\n")
	}

	// [ Client / TUI Preferences ]
	s.WriteString("\n  [ Client / TUI Preferences ]\n")
	pacingStr := "disabled"
	if m.Config.IsPacingEnabled() {
		pacingStr = "enabled (natural reading pace)"
	}
	fmt.Fprintf(&s, "    Pacing:          %s\n", pacingStr)
	remoteURL := cli.RemoteURL
	if remoteURL == "" {
		remoteURL = "http://127.0.0.1:8080 (default)"
	}
	fmt.Fprintf(&s, "    Remote Daemon:   %s\n", remoteURL)
	cliTokenStr := "(none)"
	if cli.AuthToken != "" {
		cliTokenStr = "•••••••• (configured)"
	}
	fmt.Fprintf(&s, "    Client Token:    %s\n", cliTokenStr)

	s.WriteString("\nUsage:\n")
	s.WriteString("  /config model <name>          Change LLM model\n")
	s.WriteString("  /config endpoint <url>        Change API endpoint\n")
	s.WriteString("  /config workspace <path|def>  Set workspace root directory\n")
	s.WriteString("  /config key <val|default>     Set or clear vault encryption key\n")
	s.WriteString("  /config pacing <on|off>       Toggle natural reading pace\n")
	s.WriteString("  /config remote <url>          Set default remote daemon URL\n")
	s.WriteString("  /config temp <val|default>    Set sampling temperature\n")
	s.WriteString("  /config top_p <val|default>   Set top-p sampling\n")
	s.WriteString("  /config top_k <val|default>   Set top-k sampling\n")
	s.WriteString("  /config ctx <val|default>     Set context window tokens\n")
	s.WriteString("  /config max_tokens <val|def>  Set maximum generation tokens\n")
	s.WriteString("  /config penalty <val|default> Set repeat penalty (e.g. 1.10)\n")

	return s.String()
}

type ConfigCommand struct{}

func (c *ConfigCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.ViewportOverride = m.renderConfigString()
		m.Viewport.SetContent(m.ViewportOverride)
		m.Viewport.GotoTop()
		return m, nil
	}

	if len(args) < 2 {
		m.Notification = "Usage: /config <model|endpoint|workspace|key|pacing|remote|temp|top_p|top_k|ctx|max_tokens|penalty> <value>"
		return m, nil
	}

	if m.Config.Server == nil {
		m.Config.Server = &engine.ServerConfig{}
	}
	if m.Config.Client == nil {
		m.Config.Client = &engine.ClientConfig{}
	}

	key := args[0]
	value := args[1]

	switch key {
	case "model":
		m.Config.Server.Model = value
		if op, ok := m.Provider.(*engine.OllamaProvider); ok {
			op.Model = value
		} else if op, ok := m.Provider.(*engine.OpenAIProvider); ok {
			op.Model = value
		}
		m.Notification = "Model updated to " + value
	case "endpoint":
		m.Config.Server.Endpoint = value
		if op, ok := m.Provider.(*engine.OllamaProvider); ok {
			op.Endpoint = value
		} else if op, ok := m.Provider.(*engine.OpenAIProvider); ok {
			op.Endpoint = value
		}
		m.Notification = "Endpoint updated to " + value
	case "workspace", "dir", "root", "workdir":
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" || value == "." {
			m.Config.Server.WorkspaceDir = ""
			m.Manager.RegisterDefaultTools(".")
			m.Notification = "Workspace directory reset to current directory."
		} else {
			m.Config.Server.WorkspaceDir = value
			m.Manager.RegisterDefaultTools(m.Config.GetWorkspaceDir())
			m.Notification = fmt.Sprintf("Workspace directory set to %s", m.Config.GetWorkspaceDir())
		}
	case "key", "encryption_key", "encryption":
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" || strings.ToLower(value) == "clear" || strings.ToLower(value) == "off" {
			m.Config.Server.EncryptionKey = ""
			m.Notification = "Encryption key cleared."
		} else {
			m.Config.Server.EncryptionKey = value
			m.Notification = "Encryption key updated."
		}
	case "pacing":
		switch strings.ToLower(value) {
		case "on", "true", "yes":
			pacing := true
			m.Config.Client.NaturalPacing = &pacing
			m.Notification = "Natural reading pacing enabled."
		case "off", "false", "no":
			pacing := false
			m.Config.Client.NaturalPacing = &pacing
			m.Notification = "Natural reading pacing disabled."
		default:
			m.Notification = "Usage: /config pacing <on|off>"
			return m, nil
		}
	case "remote", "daemon", "server_url", "url":
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" {
			m.Config.Client.RemoteURL = "http://127.0.0.1:8080"
			m.Notification = "Default remote daemon URL reset to http://127.0.0.1:8080"
		} else {
			m.Config.Client.RemoteURL = value
			m.Notification = "Default remote daemon URL set to " + value
		}
	case "temp", "temperature":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &engine.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.Temperature = nil
			m.Notification = "Temperature reset to default"
		} else {
			var val float64
			if _, err := fmt.Sscanf(value, "%f", &val); err != nil || val < 0 {
				m.Notification = "Invalid temperature value. Expected positive float (e.g. 0.7)"
				return m, nil
			}
			m.Config.Server.Options.Temperature = &val
			m.Notification = fmt.Sprintf("Temperature set to %.2f", val)
		}
		m.syncProviderOptions()
	case "top_p", "topp":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &engine.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.TopP = nil
			m.Notification = "Top-p reset to default"
		} else {
			var val float64
			if _, err := fmt.Sscanf(value, "%f", &val); err != nil || val < 0 || val > 1.0 {
				m.Notification = "Invalid top_p value. Expected float between 0.0 and 1.0 (e.g. 0.9)"
				return m, nil
			}
			m.Config.Server.Options.TopP = &val
			m.Notification = fmt.Sprintf("Top-p set to %.2f", val)
		}
		m.syncProviderOptions()
	case "top_k", "topk":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &engine.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.TopK = nil
			m.Notification = "Top-k reset to default"
		} else {
			var val int
			if _, err := fmt.Sscanf(value, "%d", &val); err != nil || val < 0 {
				m.Notification = "Invalid top_k value. Expected positive integer (e.g. 40)"
				return m, nil
			}
			m.Config.Server.Options.TopK = &val
			m.Notification = fmt.Sprintf("Top-k set to %d", val)
		}
		m.syncProviderOptions()
	case "ctx", "num_ctx", "context", "context_size":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &engine.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.NumCtx = nil
			m.Notification = "Context size reset to default"
		} else {
			var val int
			if _, err := fmt.Sscanf(value, "%d", &val); err != nil || val <= 0 {
				m.Notification = "Invalid context size value. Expected positive integer (e.g. 16384)"
				return m, nil
			}
			m.Config.Server.Options.NumCtx = &val
			m.Notification = fmt.Sprintf("Context size set to %d tokens", val)
		}
		m.syncProviderOptions()
	case "max_tokens", "num_predict", "tokens":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &engine.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.MaxTokens = nil
			m.Notification = "Max tokens reset to default"
		} else {
			var val int
			if _, err := fmt.Sscanf(value, "%d", &val); err != nil || val <= 0 {
				m.Notification = "Invalid max_tokens value. Expected positive integer (e.g. 2048)"
				return m, nil
			}
			m.Config.Server.Options.MaxTokens = &val
			m.Notification = fmt.Sprintf("Max tokens set to %d", val)
		}
		m.syncProviderOptions()
	case "penalty", "repeat_penalty", "repeatpenalty":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &engine.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.RepeatPenalty = nil
			m.Notification = "Repeat penalty reset to default"
		} else {
			var val float64
			if _, err := fmt.Sscanf(value, "%f", &val); err != nil || val < 0 {
				m.Notification = "Invalid repeat_penalty value. Expected positive float (e.g. 1.10)"
				return m, nil
			}
			m.Config.Server.Options.RepeatPenalty = &val
			m.Notification = fmt.Sprintf("Repeat penalty set to %.2f", val)
		}
		m.syncProviderOptions()
	default:
		m.Notification = "Unknown config key: " + key
		return m, nil
	}

	if err := m.Config.Save(); err != nil {
		m.Notification = fmt.Sprintf("Error saving config: %v", err)
	}

	if m.ViewportOverride != "" && strings.Contains(m.ViewportOverride, "Configuration") {
		m.ViewportOverride = m.renderConfigString()
		m.Viewport.SetContent(m.ViewportOverride)
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
	s.WriteString("  /pacing         Toggle natural reading pacing for LLM stream (/pacing [on|off])\n")
	s.WriteString("  /compact [hint] Summarize the current branch into a milestone Supernode (alias: /compress)\n")
	s.WriteString("  /fold [all]     Fold/unfold reasoning thought process blocks (key: Tab / Shift+Tab)\n")
	s.WriteString("  /parameters     Inspect Stable Diffusion metadata parameters for current node images (alias: /info)\n")
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

type VersionCommand struct{}

func (c *VersionCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	m.Notification = fmt.Sprintf("please version %s", engine.Version)
	return m, nil
}

type ConfirmToolCommand struct{}

func (c *ConfirmToolCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if !m.AwaitingToolConfirmation {
		m.Notification = "No tool execution is pending confirmation."
		return m, nil
	}
	m.AwaitingToolConfirmation = false
	m.IsThinking = true
	return m, m.executeToolsCmd()
}

type CancelToolCommand struct{}

func (c *CancelToolCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if !m.AwaitingToolConfirmation {
		m.Notification = "No tool execution is pending confirmation."
		return m, nil
	}
	m.AwaitingToolConfirmation = false
	m.IsThinking = true
	return m, m.cancelToolsCmd()
}

type AttachCommand struct{}

func (c *AttachCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 {
		m.Notification = "Usage: /attach <path>"
		return m, nil
	}
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		m.Notification = fmt.Sprintf("Error: file %s does not exist or is not readable", path)
		return m, nil
	}

	m.PendingImages = append(m.PendingImages, path)
	m.Notification = fmt.Sprintf("Attached image: %s", path)
	return m, nil
}

type ParametersCommand struct{}

func (c *ParametersCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	node, err := m.Manager.GetNode(m.CurrentID)
	if err != nil {
		m.Notification = "Error: node not found"
		return m, nil
	}
	if len(node.Images) == 0 {
		m.Notification = "No images attached to this node."
		return m, nil
	}

	var s strings.Builder
	s.WriteString("--- 🖼️  Attached Image Parameters ---\n\n")
	for _, imgPath := range node.Images {
		file, err := os.Open(imgPath)
		if err != nil {
			fmt.Fprintf(&s, "File: %s (Error opening file: %v)\n\n", imgPath, err)
			continue
		}
		cfg, format, err := image.DecodeConfig(file)
		if err != nil {
			fmt.Fprintf(&s, "File: %s (Error parsing format: %v)\n\n", imgPath, err)
			file.Close()
			continue
		}
		fmt.Fprintf(&s, "File: %s\n", imgPath)
		fmt.Fprintf(&s, "Format: %s\n", format)
		fmt.Fprintf(&s, "Dimensions: %dx%d\n", cfg.Width, cfg.Height)

		if strings.ToLower(format) == "png" {
			_, _ = file.Seek(0, 0)
			meta, err := engine.ExtractPNGMetadata(file)
			if err == nil {
				if params, exists := meta["parameters"]; exists {
					fmt.Fprintf(&s, "\nStable Diffusion Parameters:\n%s\n", params)
				} else {
					fmt.Fprintf(&s, "\nStable Diffusion Parameters: None found\n")
				}
			} else {
				fmt.Fprintf(&s, "\nMetadata parsing error: %v\n", err)
			}
		}
		file.Close()
		s.WriteString("\n------------------------------------\n\n")
	}

	m.ViewportOverride = s.String()
	m.Viewport.SetContent(m.ViewportOverride)
	m.Viewport.GotoTop()
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
				if node.Role == engine.RoleAssistant && m.isThoughtExpanded(node.ID) {
					hasExpanded = true
					break
				}
			}
		}
		newState := !hasExpanded
		if path, err := m.Manager.GetPath(m.CurrentID); err == nil {
			for _, node := range path {
				if node.Role == engine.RoleAssistant {
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
			if path[i].Role == engine.RoleAssistant && (path[i].Thought != "" || (path[i].Metadata != nil && path[i].Metadata["segments"] != "")) {
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
