package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/worktree"
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
	var content string
	if len(nodes) == 0 {
		content = "No nodes found in graph."
	} else {
		content = "--- Node List ---\n" + strings.Join(nodes, "\n")
	}
	m.ensureViewStack()
	m.ViewStack.Push(NewTextViewLayer(m, "nodes", "Node List", content, ""))
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
	m.Viewport.SetContent(m.generateMapString())
	m.Viewport.GotoTop()
	m.ensureViewStack()
	if m.ViewStack.Top().Name() != "map" {
		m.ViewStack.Push(newMapLayer(m))
	}
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
	if op, ok := m.Provider.(*providers.OllamaProvider); ok {
		op.Options = m.Config.Server.Options
	} else if op, ok := m.Provider.(*providers.OpenAIProvider); ok {
		op.Options = m.Config.Server.Options
	}
}

func (m *Model) renderConfigString() string {
	var s strings.Builder
	s.WriteString("--- ⚙️  Configuration & Engine State ---\n\n")

	srv := m.Config.Server
	if srv == nil {
		srv = &config.ServerConfig{}
	}
	cli := m.Config.Client
	if cli == nil {
		cli = &config.ClientConfig{}
	}

	// Active Session & Scope
	sessionStr := "Standalone (Local Embedded Engine)"
	if m.RemoteURL != "" {
		sessionStr = fmt.Sprintf("Connected (%s 🟢)", m.RemoteURL)
	}
	fmt.Fprintf(&s, "  • Active Session:  %s\n", sessionStr)
	if m.Config.IsWorkspaceActive() {
		wsFile := filepath.Join(m.Config.WorkspaceRoot, ".please", "config.json")
		fmt.Fprintf(&s, "  • Active Scope:    Workspace (%s)\n", m.Config.WorkspaceRoot)
		fmt.Fprintf(&s, "  • Workspace File:  %s\n", wsFile)
		if gDir, err := config.GetGlobalPleaseDir(); err == nil {
			fmt.Fprintf(&s, "  • Global File:     %s\n\n", filepath.Join(gDir, "config.json"))
		} else {
			s.WriteString("\n")
		}
	} else {
		if gDir, err := config.GetGlobalPleaseDir(); err == nil {
			fmt.Fprintf(&s, "  • Active Scope:    Global Anchor (~/.please)\n")
			fmt.Fprintf(&s, "  • Global File:     %s\n\n", filepath.Join(gDir, "config.json"))
		}
	}

	// [ Server / Engine Backend ]
	s.WriteString("  [ Server / Engine Backend ]\n")
	providerStr := srv.Provider
	if providerStr == "" {
		providerStr = "ollama"
	}
	providerBadge := m.Config.OriginBadge("server.provider")
	if m.Config.GetOrigin("server.model") != "default" {
		providerBadge = m.Config.OriginBadge("server.model")
	}
	fmt.Fprintf(&s, "    Provider:        %-32s %s\n", fmt.Sprintf("%s (%s)", providerStr, srv.Model), providerBadge)
	fmt.Fprintf(&s, "    Endpoint:        %-32s %s\n", srv.Endpoint, m.Config.OriginBadge("server.endpoint"))
	storageType := srv.StorageType
	if storageType == "" {
		storageType = "sqlite"
	}
	fmt.Fprintf(&s, "    Vault:           %-32s %s\n", fmt.Sprintf("%s (%s)", srv.VaultPath, storageType), m.Config.OriginBadge("server.vault_path"))
	wsStr := srv.WorkspaceDir
	if wsStr == "" {
		wsStr = "(current directory)"
	}
	fmt.Fprintf(&s, "    Workspace:       %-32s %s\n", wsStr, m.Config.OriginBadge("server.workspace_dir"))
	encStr := "(disabled)"
	if srv.EncryptionKey != "" {
		encStr = "•••••••• (configured)"
	}
	fmt.Fprintf(&s, "    Encryption:      %-32s %s\n", encStr, m.Config.OriginBadge("server.encryption_key"))
	authStr := "disabled (open local)"
	if srv.AuthToken != "" {
		authStr = "enabled (Bearer token active)"
	}
	fmt.Fprintf(&s, "    Authentication:  %s\n", authStr)
	sandboxStr := srv.SandboxPolicy
	if sandboxStr == "" {
		sandboxStr = "standard (default)"
	}
	fmt.Fprintf(&s, "    Sandbox Policy:  %-32s %s\n", sandboxStr, m.Config.OriginBadge("server.sandbox_policy"))
	signatStr := "disabled (pure prompt, silent metadata derivation)"
	if m.Config.EnableSignatSteering() {
		signatStr = "enabled (layered Genesis prompt steering)"
	}
	fmt.Fprintf(&s, "    Signat Steering:    %s %s\n", signatStr, m.Config.OriginBadge("server.signat_steering"))
	telemStr := "disabled (pure human turns)"
	if m.Config.EnableAmbientTelemetry() {
		telemStr = "enabled (bounded XML leaf envelope)"
	}
	fmt.Fprintf(&s, "    Ambient Telemetry:  %s %s\n", telemStr, m.Config.OriginBadge("server.ambient_telemetry"))

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
		if srv.Options.MinP != nil {
			fmt.Fprintf(&s, "      Min P:         %.2f\n", *srv.Options.MinP)
		} else {
			s.WriteString("      Min P:         (default)\n")
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
		if srv.Options.RepeatLastN != nil {
			fmt.Fprintf(&s, "      Repeat Last N: %d tokens\n", *srv.Options.RepeatLastN)
		} else {
			s.WriteString("      Repeat Last N: (default)\n")
		}
		if srv.Options.FrequencyPenalty != nil {
			fmt.Fprintf(&s, "      Freq Penalty:  %.2f\n", *srv.Options.FrequencyPenalty)
		} else {
			s.WriteString("      Freq Penalty:  (default)\n")
		}
	} else {
		s.WriteString("      (All provider defaults)\n")
	}

	// [ Client / TUI Preferences ]
	s.WriteString("\n  [ Client / TUI Preferences ]\n")
	pacingStr := "disabled"
	if m.Config.EnableNaturalPacing() {
		pacingStr = "enabled (natural reading pace)"
	}
	fmt.Fprintf(&s, "    Pacing:          %-32s %s\n", pacingStr, m.Config.OriginBadge("client.natural_pacing"))
	bellStr := "disabled"
	if m.Config.EnableBellOnTurnComplete() {
		bellStr = "enabled (ASCII 0x07 / \\a)"
	}
	fmt.Fprintf(&s, "    Terminal Bell:   %-32s %s\n", bellStr, m.Config.OriginBadge("client.bell_on_turn_complete"))
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
	s.WriteString("  /config bell <on|off>         Toggle terminal bell on turn completion\n")
	s.WriteString("  /config remote <url>          Set default remote daemon URL\n")
	s.WriteString("  /config temp <val|default>    Set sampling temperature\n")
	s.WriteString("  /config top_p <val|default>   Set top-p sampling\n")
	s.WriteString("  /config top_k <val|default>   Set top-k sampling\n")
	s.WriteString("  /config min_p <val|default>   Set min-p sampling (e.g. 0.05)\n")
	s.WriteString("  /config ctx <val|default>     Set context window tokens\n")
	s.WriteString("  /config max_tokens <val|def>  Set maximum generation tokens\n")
	s.WriteString("  /config penalty <val|default> Set repeat penalty (e.g. 1.05)\n")
	s.WriteString("  /config last_n <val|default>  Set repeat penalty lookback tokens (e.g. 128)\n")
	s.WriteString("  /config freq <val|default>    Set frequency penalty (e.g. 0.15)\n")
	s.WriteString("  /config sandbox <policy>      Set sandbox policy (strict|standard|permissive)\n")
	s.WriteString("  /config signats <on|off>      Toggle signat emoji steering\n")
	s.WriteString("  /config telemetry <on|off>    Toggle ambient environmental telemetry\n")

	return s.String()
}

type ConfigCommand struct{}

func (c *ConfigCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "config", "Configuration", m.renderConfigString(), ""))
		return m, nil
	}

	if len(args) < 2 {
		m.Notification = "Usage: /config <model|endpoint|workspace|key|pacing|remote|temp|top_p|top_k|min_p|ctx|max_tokens|penalty|last_n|freq|sandbox|signats|telemetry> <value>"
		return m, nil
	}

	if m.Config.Server == nil {
		m.Config.Server = &config.ServerConfig{}
	}
	if m.Config.Client == nil {
		m.Config.Client = &config.ClientConfig{}
	}

	key := strings.ToLower(strings.TrimSpace(args[0]))
	value := strings.Trim(strings.TrimSpace(strings.Join(args[1:], " ")), "\"'")

	switch key {
	case "model":
		m.Config.Server.Model = value
		if op, ok := m.Provider.(*providers.OllamaProvider); ok {
			op.Model = value
		} else if op, ok := m.Provider.(*providers.OpenAIProvider); ok {
			op.Model = value
		}
		m.Notification = "Model updated to " + value
	case "endpoint":
		if op, ok := m.Provider.(*providers.OllamaProvider); ok {
			value = providers.NormalizeOllamaEndpoint(value)
			op.Endpoint = value
		} else if op, ok := m.Provider.(*providers.OpenAIProvider); ok {
			value = providers.NormalizeOpenAIEndpoint(value)
			op.Endpoint = value
		}
		m.Config.Server.Endpoint = value
		m.Notification = "Endpoint updated to " + value
	case "workspace", "dir", "root", "workdir":
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" || value == "." || value == "" {
			m.Config.Server.WorkspaceDir = ""
			m.Manager.RegisterDefaultTools(".")
			m.Notification = "Workspace directory reset to current directory."
		} else {
			m.Config.Server.WorkspaceDir = value
			m.Manager.RegisterDefaultTools(m.Config.GetWorkspaceDir())
			m.Notification = fmt.Sprintf("Workspace directory set to %s", m.Config.GetWorkspaceDir())
		}
	case "key", "encryption_key", "encryption":
		valLower := strings.ToLower(value)
		if value == "" || valLower == "default" || valLower == "reset" || valLower == "none" || valLower == "clear" || valLower == "off" {
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
	case "bell", "bell_on_turn_complete":
		switch strings.ToLower(value) {
		case "on", "true", "yes":
			bell := true
			m.Config.Client.BellOnTurnComplete = &bell
			m.Notification = "🔔 Terminal bell enabled (ASCII 0x07 / \\a)"
		case "off", "false", "no":
			bell := false
			m.Config.Client.BellOnTurnComplete = &bell
			m.Notification = "🔕 Terminal bell disabled"
		default:
			m.Notification = "Usage: /config bell <on|off>"
			return m, nil
		}
	case "worktree", "worktrees", "worktree_isolation":
		switch strings.ToLower(value) {
		case "on", "true", "yes", "enable", "enabled":
			wt := true
			m.Config.Server.WorktreeIsolation = &wt
			m.Notification = "Git worktree sandboxing enabled."
		case "off", "false", "no", "disable", "disabled":
			wt := false
			m.Config.Server.WorktreeIsolation = &wt
			m.Notification = "Git worktree sandboxing disabled."
		default:
			m.Notification = "Usage: /config worktree <on|off>"
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
			m.Config.Server.Options = &domain.ModelOptions{}
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
			m.Config.Server.Options = &domain.ModelOptions{}
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
			m.Config.Server.Options = &domain.ModelOptions{}
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
	case "min_p", "minp":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &domain.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.MinP = nil
			m.Notification = "Min-p reset to default"
		} else {
			var val float64
			if _, err := fmt.Sscanf(value, "%f", &val); err != nil || val < 0 || val > 1.0 {
				m.Notification = "Invalid min_p value. Expected float between 0.0 and 1.0 (e.g. 0.05)"
				return m, nil
			}
			m.Config.Server.Options.MinP = &val
			m.Notification = fmt.Sprintf("Min-p set to %.2f", val)
		}
		m.syncProviderOptions()
	case "ctx", "num_ctx", "context", "context_size":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &domain.ModelOptions{}
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
			m.Config.Server.Options = &domain.ModelOptions{}
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
			m.Config.Server.Options = &domain.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.RepeatPenalty = nil
			m.Notification = "Repeat penalty reset to default"
		} else {
			var val float64
			if _, err := fmt.Sscanf(value, "%f", &val); err != nil || val < 0 {
				m.Notification = "Invalid repeat_penalty value. Expected positive float (e.g. 1.05)"
				return m, nil
			}
			m.Config.Server.Options.RepeatPenalty = &val
			m.Notification = fmt.Sprintf("Repeat penalty set to %.2f", val)
		}
		m.syncProviderOptions()
	case "last_n", "repeat_last_n", "repeatlastn":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &domain.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.RepeatLastN = nil
			m.Notification = "Repeat last N reset to default"
		} else {
			var val int
			if _, err := fmt.Sscanf(value, "%d", &val); err != nil || val < 0 {
				m.Notification = "Invalid repeat_last_n value. Expected positive integer (e.g. 128)"
				return m, nil
			}
			m.Config.Server.Options.RepeatLastN = &val
			m.Notification = fmt.Sprintf("Repeat last N set to %d tokens", val)
		}
		m.syncProviderOptions()
	case "freq", "freq_penalty", "frequency_penalty", "freqpenalty":
		if m.Config.Server.Options == nil {
			m.Config.Server.Options = &domain.ModelOptions{}
		}
		if strings.ToLower(value) == "default" || strings.ToLower(value) == "reset" || strings.ToLower(value) == "none" {
			m.Config.Server.Options.FrequencyPenalty = nil
			m.Notification = "Frequency penalty reset to default"
		} else {
			var val float64
			if _, err := fmt.Sscanf(value, "%f", &val); err != nil || val < -2.0 || val > 2.0 {
				m.Notification = "Invalid frequency_penalty value. Expected float between -2.0 and 2.0 (e.g. 0.15)"
				return m, nil
			}
			m.Config.Server.Options.FrequencyPenalty = &val
			m.Notification = fmt.Sprintf("Frequency penalty set to %.2f", val)
		}
		m.syncProviderOptions()
	case "sandbox", "sandbox_policy", "policy":
		pol := strings.ToLower(value)
		switch pol {
		case "default", "reset":
			m.Config.Server.SandboxPolicy = ""
			m.Notification = "Sandbox policy reset to standard default"
		case string(domain.SandboxPolicyStrict), string(domain.SandboxPolicyStandard), string(domain.SandboxPolicyPermissive):
			m.Config.Server.SandboxPolicy = pol
			m.Notification = fmt.Sprintf("Sandbox policy set to '%s'", pol)
		default:
			m.Notification = "Invalid sandbox policy. Expected 'strict', 'standard', or 'permissive'"
			return m, nil
		}
	case "signats", "signat", "emojis", "signat_steering":
		switch strings.ToLower(value) {
		case "on", "true", "yes", "enable":
			steering := true
			m.Config.Server.SignatSteering = &steering
			if m.Manager != nil {
				m.Manager.SignatSteering = true
			}
			m.Notification = "Signat emoji steering enabled."
		case "off", "false", "no", "disable":
			steering := false
			m.Config.Server.SignatSteering = &steering
			if m.Manager != nil {
				m.Manager.SignatSteering = false
			}
			m.Notification = "Signat emoji steering disabled."
		default:
			m.Notification = "Usage: /config signats <on|off>"
			return m, nil
		}
	case "telemetry", "ambient_telemetry":
		switch strings.ToLower(value) {
		case "on", "true", "yes", "enable":
			enabled := true
			m.Config.Server.AmbientTelemetry = &enabled
			if m.Manager != nil {
				m.Manager.AmbientTelemetry = true
			}
			m.Notification = "Ambient environmental telemetry enabled."
		case "off", "false", "no", "disable":
			enabled := false
			m.Config.Server.AmbientTelemetry = &enabled
			if m.Manager != nil {
				m.Manager.AmbientTelemetry = false
			}
			m.Notification = "Ambient environmental telemetry disabled."
		default:
			m.Notification = "Usage: /config telemetry <on|off>"
			return m, nil
		}
	default:
		m.Notification = "Unknown config key: " + key
		return m, nil
	}

	isProject := true
	originKey := "server." + key
	switch key {
	case "bell", "bell_on_turn_complete":
		isProject = false
		originKey = "client.bell_on_turn_complete"
	case "pacing":
		isProject = false
		originKey = "client.natural_pacing"
	case "remote", "daemon", "server_url", "url":
		isProject = false
		originKey = "client.remote_url"
	case "key", "encryption_key", "encryption":
		isProject = false
		originKey = "server.encryption_key"
	case "model":
		originKey = "server.model"
	case "provider":
		originKey = "server.provider"
	case "endpoint":
		originKey = "server.endpoint"
	case "signats":
		originKey = "server.signat_steering"
	case "telemetry", "ambient_telemetry":
		originKey = "server.ambient_telemetry"
	}
	m.Config.RecordOrigin(originKey, isProject)

	if m.Config.ReadOnly {
		m.Notification = "Configuration updated in-memory only (saving disabled for external config)."
	} else if savedPath, err := m.Config.SaveScoped(isProject); err != nil {
		m.Notification = fmt.Sprintf("Error saving config: %v", err)
	} else {
		m.Notification += fmt.Sprintf(" (saved to %s)", savedPath)
	}

	if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().Name() == "config" {
		m.ViewStack.Pop()
		m.ViewStack.Push(NewTextViewLayer(m, "config", "Configuration", m.renderConfigString(), ""))
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

type SandboxCommand struct{}

func (c *SandboxCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Config == nil {
		m.Config = config.NewDefaultConfig()
	}
	if m.Config.Server == nil {
		m.Config.Server = &config.ServerConfig{}
	}

	if len(args) == 0 {
		var s strings.Builder
		curPolicy := m.Config.GetSandboxPolicy()
		if curPolicy == "" {
			curPolicy = string(domain.SandboxPolicyStandard)
		}

		s.WriteString("--- 🛡️ Please Sandbox Security Perimeter ---\n\n")
		s.WriteString(fmt.Sprintf("Active Sandbox Policy: %s\n\n", strings.ToUpper(curPolicy)))

		switch curPolicy {
		case string(domain.SandboxPolicyStrict):
			s.WriteString("Tier: STRICT [🔒]\n")
			s.WriteString("  • Complete read-only safety; zero workspace mutations or shell execution permitted.\n")
			s.WriteString("  • Active categories: Sensory (read-only discovery tools).\n\n")
		case string(domain.SandboxPolicyPermissive):
			s.WriteString("Tier: PERMISSIVE [⚠️]\n")
			s.WriteString("  • Full compute capability with interactive consent gates.\n")
			s.WriteString("  • Raw shell execution enabled; every execution requires operator confirmation.\n")
			s.WriteString("  • Active categories: Sensory, Mutate, and Execute.\n\n")
		case string(domain.SandboxPolicyStandard):
			fallthrough
		default:
			s.WriteString("Tier: STANDARD [🛡️] (Default)\n")
			s.WriteString("  • Safe workspace editing permitted; host compute / raw shell execution blocked.\n")
			s.WriteString("  • Active categories: Sensory and Mutate (file edits/writes).\n\n")
		}

		s.WriteString("Available Policy Modes:\n")
		s.WriteString("  /sandbox strict       Demote to read-only inspection (drops edits & shell)\n")
		s.WriteString("  /sandbox standard     Restore safe workspace edits (blocks raw shell)\n")
		s.WriteString("  /sandbox permissive   Enable host command execution with mandatory consent\n\n")

		s.WriteString("Active Permitted Tools:\n")
		if m.Manager != nil && m.Manager.Registry != nil {
			activeTools := m.Manager.Registry.GetToolsForPolicy(curPolicy)
			for _, t := range activeTools {
				interactiveBadge := ""
				if t.Interactive {
					interactiveBadge = " [consent required]"
				}
				s.WriteString(fmt.Sprintf("  • %-18s (%-7s)%s - %s\n", t.Name, t.Category, interactiveBadge, t.Description))
			}
		}

		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "sandbox", "Sandbox Security Perimeter", s.String(), ""))
		return m, nil
	}

	targetPolicy := strings.ToLower(strings.TrimSpace(args[0]))
	switch targetPolicy {
	case string(domain.SandboxPolicyStrict), string(domain.SandboxPolicyStandard), string(domain.SandboxPolicyPermissive):
		m.Config.Server.SandboxPolicy = targetPolicy
		m.Notification = fmt.Sprintf("Sandbox policy switched to %s", strings.ToUpper(targetPolicy))
		m.Config.RecordOrigin("server.sandbox_policy", true)
		if !m.Config.ReadOnly {
			if savedPath, err := m.Config.SaveScoped(true); err == nil {
				m.Notification += fmt.Sprintf(" (saved to %s)", savedPath)
			}
		}
		if m.ViewStack != nil && m.ViewStack.Top() != nil && m.ViewStack.Top().Name() == "sandbox" {
			m.ViewStack.Pop()
			return c.Execute(m, nil)
		}
		return m, nil
	case "default", "reset":
		m.Config.Server.SandboxPolicy = ""
		m.Notification = "Sandbox policy reset to standard default"
		m.Config.RecordOrigin("server.sandbox_policy", true)
		if !m.Config.ReadOnly {
			if savedPath, err := m.Config.SaveScoped(true); err == nil {
				m.Notification += fmt.Sprintf(" (saved to %s)", savedPath)
			}
		}
		return m, nil
	default:
		m.Notification = fmt.Sprintf("Unknown sandbox policy %q. Expected 'strict', 'standard', or 'permissive'", targetPolicy)
		return m, nil
	}
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
	if !m.hasActiveOverlay("confirm_tool") {
		m.Notification = "No tool execution is pending confirmation."
		return m, nil
	}
	m.ViewStack.Pop()
	m.IsThinking = true
	return m, m.executeToolsCmd()
}

type CancelToolCommand struct{}

func (c *CancelToolCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if !m.hasActiveOverlay("confirm_tool") {
		m.Notification = "No tool execution is pending confirmation."
		return m, nil
	}
	m.ViewStack.Pop()
	m.IsThinking = true
	return m, m.cancelToolsCmd()
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

type SessionCommand struct{}

func (c *SessionCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		headInfo := m.CurrentID
		if len(headInfo) > 8 {
			headInfo = headInfo[:8]
		}
		m.Notification = fmt.Sprintf("Active session: %q (head: %s)", m.SessionID, headInfo)
		return m, nil
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		if m.Manager == nil || m.Manager.Storage == nil {
			m.Notification = "Storage not initialized"
			return m, nil
		}
		sessions, err := m.Manager.Storage.ListSessions()
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to list sessions: %v", err)
			return m, nil
		}
		var content string
		if len(sessions) == 0 {
			content = "--- Sessions ---\nNo saved sessions found."
		} else {
			var sb strings.Builder
			sb.WriteString("--- Active Sessions ---\n")
			for id, headID := range sessions {
				marker := "  "
				if id == m.SessionID {
					marker = "➜ "
				}
				shortHead := headID
				if len(shortHead) > 8 {
					shortHead = shortHead[:8]
				}
				sb.WriteString(fmt.Sprintf("%s%-16s (head: %s)\n", marker, id, shortHead))
			}
			content = sb.String()
		}
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "sessions", "Active Sessions", content, ""))
		return m, nil

	case "switch":
		if len(args) < 2 {
			m.Notification = "Usage: /session switch <name>"
			return m, nil
		}
		targetSession := args[1]
		if targetSession == "" {
			m.Notification = "Session name cannot be empty"
			return m, nil
		}

		m.SessionID = targetSession
		if rdp, ok := m.Provider.(*providers.RemoteDaemonProvider); ok {
			rdp.SessionID = targetSession
		}
		if lhp, ok := m.Provider.(*engine.LocalHarnessProvider); ok {
			lhp.SetSessionID(targetSession)
		}
		if rds, ok := m.Manager.Storage.(*storage.RemoteDaemonStorage); ok {
			rds.SessionID = targetSession
		}

		headID, err := m.Manager.Storage.GetSessionHead(targetSession)
		if err == nil && headID != "" {
			if node, err := m.Manager.GetNode(headID); err == nil {
				m.navigateToNode(node)
				shortID := headID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				m.Notification = fmt.Sprintf("Switched to session %q (resumed at %s)", targetSession, shortID)
				return m, nil
			}
		}

		_ = m.Manager.Storage.SaveSessionHead(targetSession, m.CurrentID)
		m.Notification = fmt.Sprintf("Switched to session %q (anchored at current node)", targetSession)
		return m, nil

	default:
		// Shortcut: `/session <name>` behaves like `/session switch <name>`
		targetSession := args[0]
		m.SessionID = targetSession
		if rdp, ok := m.Provider.(*providers.RemoteDaemonProvider); ok {
			rdp.SessionID = targetSession
		}
		if lhp, ok := m.Provider.(*engine.LocalHarnessProvider); ok {
			lhp.SetSessionID(targetSession)
		}
		if rds, ok := m.Manager.Storage.(*storage.RemoteDaemonStorage); ok {
			rds.SessionID = targetSession
		}

		headID, err := m.Manager.Storage.GetSessionHead(targetSession)
		if err == nil && headID != "" {
			if node, err := m.Manager.GetNode(headID); err == nil {
				m.navigateToNode(node)
				shortID := headID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				m.Notification = fmt.Sprintf("Switched to session %q (resumed at %s)", targetSession, shortID)
				return m, nil
			}
		}

		_ = m.Manager.Storage.SaveSessionHead(targetSession, m.CurrentID)
		m.Notification = fmt.Sprintf("Switched to session %q (anchored at current node)", targetSession)
		return m, nil
	}
}

type WorktreeCommand struct{}

func (c *WorktreeCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	cfgDir, _ := config.GetConfigDir()
	wsDir := m.Config.GetWorkspaceDir()
	wtMgr := worktree.NewManager(cfgDir, wsDir)

	if len(args) == 0 {
		var sb strings.Builder
		sb.WriteString("--- Git Worktree Sandboxing ---\n")
		enabled := m.Config.EnableWorktreeIsolation()
		gitAvail := wtMgr.IsGitAvailable()
		isRepo := wtMgr.IsGitRepo()

		isolationStatus := "disabled (opt-in)"
		if enabled {
			isolationStatus = "enabled"
		}
		sb.WriteString(fmt.Sprintf("  Configuration: %s\n", isolationStatus))
		if !gitAvail {
			sb.WriteString("  Status:        inactive (git binary not found in PATH)\n")
		} else if !isRepo {
			sb.WriteString("  Status:        inactive (workspace is not a git repository)\n")
		} else {
			sb.WriteString("  Status:        active\n")
			topLevel, _ := wtMgr.GetTopLevel()
			sb.WriteString(fmt.Sprintf("  Primary Repo:  %s\n", topLevel))
			activeDir, _ := wtMgr.GetWorktreeDir(m.SessionID)
			sb.WriteString(fmt.Sprintf("  Session:       %s\n", m.SessionID))
			sb.WriteString(fmt.Sprintf("  Active Dir:    %s\n", activeDir))
			if m.SessionID != "" && m.SessionID != "main" {
				sb.WriteString(fmt.Sprintf("  Branch:        please/%s\n", m.SessionID))
			} else {
				sb.WriteString("  Branch:        (primary checkout)\n")
			}
		}
		sb.WriteString("\nCommands:\n  /worktree list           List all worktrees\n  /worktree remove <name>  Remove worktree checkout\n  /config worktree on|off  Toggle isolation\n")
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "worktree", "Worktree Status", sb.String(), ""))
		return m, nil
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		if !wtMgr.IsGitAvailable() {
			m.Notification = "Git binary not found in PATH"
			return m, nil
		}
		if !wtMgr.IsGitRepo() {
			m.Notification = "Workspace is not inside a git repository"
			return m, nil
		}
		list, err := wtMgr.ListWorktrees()
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to list worktrees: %v", err)
			return m, nil
		}
		var sb strings.Builder
		sb.WriteString("--- Active Git Worktrees ---\n")
		for _, wt := range list {
			marker := "  "
			if wt.SessionID == m.SessionID {
				marker = "➜ "
			}
			primaryTag := ""
			if wt.IsPrimary {
				primaryTag = " [primary]"
			}
			sb.WriteString(fmt.Sprintf("%s%-16s %-24s %s%s\n", marker, wt.SessionID, wt.Branch, wt.Path, primaryTag))
		}
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "worktree", "Active Git Worktrees", sb.String(), ""))
		return m, nil

	case "remove", "rm", "delete":
		if len(args) < 2 {
			m.Notification = "Usage: /worktree remove <session-name>"
			return m, nil
		}
		target := args[1]
		if target == "main" {
			m.Notification = "Cannot remove primary repository worktree for 'main'"
			return m, nil
		}
		if err := wtMgr.RemoveWorktree(target, true, true); err != nil {
			m.Notification = fmt.Sprintf("Failed to remove worktree: %v", err)
			return m, nil
		}
		m.Notification = fmt.Sprintf("Worktree for session %q removed successfully", target)
		return m, nil

	default:
		m.Notification = "Usage: /worktree [list|remove <session>]"
		return m, nil
	}
}

type MemoriesCommand struct{}

func (c *MemoriesCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Manager == nil || m.Manager.Storage == nil {
		m.Notification = "Storage not initialized"
		return m, nil
	}

	memStore, ok := m.Manager.Storage.(storage.MemoryStore)
	if !ok {
		m.Notification = "Persistent agent memory requires SQLite storage (.db)"
		return m, nil
	}

	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "diag", "diagnose", "stats":
		diag, err := memStore.DiagnoseMemories("", "")
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to diagnose memories: %v", err)
			return m, nil
		}
		var sb strings.Builder
		sb.WriteString("--- 🧠 Memory Vault Telemetry & Health (ADR 014) ---\n\n")
		sb.WriteString(fmt.Sprintf("Total Memories:    %d\n", diag.TotalMemories))
		sb.WriteString(fmt.Sprintf("Storage Footprint: %.2f KB (%d bytes)\n\n", float64(diag.StorageBytes)/1024.0, diag.StorageBytes))

		sb.WriteString("Scope Distribution:\n")
		for sc, count := range diag.ByScope {
			sb.WriteString(fmt.Sprintf("  • %-12s: %d\n", sc, count))
		}

		sb.WriteString("\nCategory Distribution:\n")
		for cat, count := range diag.ByCategory {
			sb.WriteString(fmt.Sprintf("  • %-14s: %d\n", cat, count))
		}

		if len(diag.MostAccessed) > 0 {
			sb.WriteString("\n🔥 Most Accessed Memories:\n")
			for i, mem := range diag.MostAccessed {
				if i >= 5 {
					break
				}
				sb.WriteString(fmt.Sprintf("  %d. %s (%s, %d accesses)\n", i+1, mem.Key, mem.Scope, mem.AccessCount))
			}
		}

		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "memories_stats", "Memory Diagnostic Telemetry", sb.String(), ""))
		return m, nil

	case "inspect", "show", "get":
		if len(args) < 2 {
			m.Notification = "Usage: /memories inspect <key>"
			return m, nil
		}
		key := args[1]
		mem, _ := memStore.GetMemory(storage.ScopeWorkspace, m.SessionID, key)
		if mem == nil {
			mem, _ = memStore.GetMemory(storage.ScopeGlobal, "", key)
		}
		if mem == nil {
			// Fuzzy / prefix search
			candidates, _ := memStore.QueryMemories(storage.MemoryFilter{Limit: 100})
			lower := strings.ToLower(key)
			for _, c := range candidates {
				if strings.Contains(strings.ToLower(c.Key), lower) {
					found := c
					mem = &found
					break
				}
			}
		}
		if mem == nil {
			m.Notification = fmt.Sprintf("Memory %q not found", key)
			return m, nil
		}

		m.ViewMode = ModeMemories
		m.MemoryDeck = []storage.Memory{*mem}
		m.MemoryDeckIndex = 0
		m.MemoryDetailCard = mem
		m.Viewport.SetContent(m.renderMemoriesView())
		m.Viewport.GotoTop()
		m.ensureViewStack()
		if m.ViewStack.Top().Name() != "memories" {
			m.ViewStack.Push(newMemoriesDeckLayer(m))
		}
		if m.ViewStack.Top().Name() != "memory_card" {
			m.ViewStack.Push(newMemoryCardOverlay(m, mem))
		}
		return m, nil

	default:
		filter := storage.MemoryFilter{Limit: 100}
		if sub == "search" || sub == "find" || sub == "q" {
			if len(args) > 1 {
				filter.Query = strings.Join(args[1:], " ")
			}
		} else if len(args) > 0 && !strings.HasPrefix(args[0], "/") {
			filter.Query = strings.Join(args, " ")
		}

		mems, err := memStore.QueryMemories(filter)
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to query memories: %v", err)
			return m, nil
		}

		m.ViewMode = ModeMemories
		m.MemoryDeck = mems
		m.MemoryDeckIndex = 0
		m.MemoryDetailCard = nil
		m.MemoryDeckFilter = filter.Query
		m.Viewport.SetContent(m.renderMemoriesView())
		m.Viewport.GotoTop()
		m.ensureViewStack()
		if m.ViewStack.Top().Name() != "memories" {
			m.ViewStack.Push(newMemoriesDeckLayer(m))
		}
		return m, nil
	}
}
