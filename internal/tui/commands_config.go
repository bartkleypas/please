package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/providers"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) syncProviderOptions() {
	if m.Config.Server == nil {
		return
	}
	target := m.Provider
	if lhp, ok := m.Provider.(*engine.LocalHarnessProvider); ok {
		target = lhp.RawProvider()
	}
	if op, ok := target.(*providers.OllamaProvider); ok {
		op.Options = m.Config.Server.Options
	} else if op, ok := target.(*providers.OpenAIProvider); ok {
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
