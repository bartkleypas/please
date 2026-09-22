package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/tools"
)

// CurrentConfigVersion is the current schema version for config.json
const CurrentConfigVersion = 2

// ModelOptions holds model inference and sampling parameters (aliased from internal/providers)
type ModelOptions = providers.ModelOptions

// ServerConfig holds settings for running the engine daemon / standalone backend
type ServerConfig struct {
	Host              string        `json:"host,omitempty"`
	Port              int           `json:"port,omitempty"`
	Provider          string        `json:"provider,omitempty"`
	APIKey            string        `json:"api_key,omitempty"`
	Model             string        `json:"model,omitempty"`
	Endpoint          string        `json:"endpoint,omitempty"`
	VaultPath         string        `json:"vault_path,omitempty"`
	Vault             string        `json:"vault,omitempty"`
	StorageType       string        `json:"storage_type,omitempty"` // "jsonl" or "sqlite"
	EncryptionKey     string        `json:"encryption_key,omitempty"`
	WorkspaceDir      string        `json:"workspace_dir,omitempty"`
	Workspace         string        `json:"workspace,omitempty"`
	AuthToken         string        `json:"auth_token,omitempty"`
	TLSCertFile       string        `json:"tls_cert_file,omitempty"`
	TLSKeyFile        string        `json:"tls_key_file,omitempty"`
	SandboxPolicy     string        `json:"sandbox_policy,omitempty"` // "strict", "standard", "permissive"
	MaxToolDepth      *int          `json:"max_tool_depth,omitempty"`
	SignatSteering    *bool         `json:"signat_steering,omitempty"`
	AmbientTelemetry  *bool         `json:"ambient_telemetry,omitempty"`
	WorktreeIsolation *bool         `json:"worktree_isolation,omitempty"`
	Options           *ModelOptions `json:"options,omitempty"`
}

// ClientConfig holds settings for connecting the TUI to a remote daemon
type ClientConfig struct {
	RemoteURL          string `json:"remote_url,omitempty"`
	AuthToken          string `json:"auth_token,omitempty"`
	CACertPath         string `json:"ca_cert_path,omitempty"`
	NaturalPacing      *bool  `json:"natural_pacing,omitempty"`
	BellOnTurnComplete *bool  `json:"bell_on_turn_complete,omitempty"`
	Bell               *bool  `json:"bell,omitempty"`
	Session            string `json:"session,omitempty"`
}

// Config is the top-level configuration container (v2 schema)
type Config struct {
	Version       int               `json:"version"`
	Mode          string            `json:"mode,omitempty"` // "standalone", "server", "client"
	Server        *ServerConfig     `json:"server"`
	Client        *ClientConfig     `json:"client"`
	ReadOnly      bool              `json:"-"` // Prevents persisting mutations to disk when loaded via external file
	WorkspaceRoot string            `json:"-"` // Root path of active workspace, empty if running in global mode
	Origins       map[string]string `json:"-"` // Tracks setting provenance: "workspace", "global", "workspace anchor", "default"
}

// legacyV1Config mirrors the flat v1 schema for migration
type legacyV1Config struct {
	Provider           string        `json:"provider"`
	APIKey             string        `json:"api_key"`
	Model              string        `json:"model"`
	Endpoint           string        `json:"endpoint"`
	VaultPath          string        `json:"vault_path"`
	Vault              string        `json:"vault"`
	StorageType        string        `json:"storage_type"`
	EncryptionKey      string        `json:"encryption_key"`
	NaturalPacing      *bool         `json:"natural_pacing"`
	Options            *ModelOptions `json:"options"`
	WorkspaceDir       string        `json:"workspace_dir"`
	Workspace          string        `json:"workspace"`
	AuthToken          string        `json:"auth_token"`
	TLSCertFile        string        `json:"tls_cert_file"`
	TLSKeyFile         string        `json:"tls_key_file"`
	SandboxPolicy      string        `json:"sandbox_policy"`
	SignatSteering     *bool         `json:"signat_steering"`
	AmbientTelemetry   *bool         `json:"ambient_telemetry"`
	BellOnTurnComplete *bool         `json:"bell_on_turn_complete"`
	Bell               *bool         `json:"bell"`
}

// GetWorkspaceDir returns the resolved absolute workspace directory from ServerConfig.
func (c *Config) GetWorkspaceDir() string {
	if c == nil || c.Server == nil || c.Server.WorkspaceDir == "" {
		return "."
	}
	dir := os.ExpandEnv(c.Server.WorkspaceDir)
	if strings.HasPrefix(dir, "~/") || dir == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if dir == "~" {
				dir = home
			} else {
				dir = filepath.Join(home, dir[2:])
			}
		}
	}
	dir = filepath.Clean(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// GetVaultPath returns the resolved vault path from ServerConfig, expanding ~ and environment variables.
func (s *ServerConfig) GetVaultPath() string {
	if s == nil || s.VaultPath == "" {
		return ""
	}
	path := os.ExpandEnv(s.VaultPath)
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	return filepath.Clean(path)
}

// GetVaultPath returns the resolved vault path across configuration, expanding ~ and environment variables.
func (c *Config) GetVaultPath() string {
	if c == nil || c.Server == nil {
		return ""
	}
	return c.Server.GetVaultPath()
}

// IsWorkspaceActive returns true if the configuration is running inside an initialized workspace.
func (c *Config) IsWorkspaceActive() bool {
	return c != nil && c.WorkspaceRoot != ""
}

// Provenance origins for configuration values.
const (
	OriginEnv       = "env"
	OriginWorkspace = "workspace"
	OriginAnchor    = "workspace anchor"
	OriginGlobal    = "global"
	OriginDefault   = "default"
)

// GetOrigin returns the origin of a configuration field ("workspace", "workspace anchor", "global", or "default").
func (c *Config) GetOrigin(key string) string {
	if c == nil || c.Origins == nil {
		return OriginDefault
	}
	if origin, ok := c.Origins[key]; ok && origin != "" {
		return origin
	}
	return OriginDefault
}

// OriginBadge returns a formatted provenance label for display in the TUI.
func (c *Config) OriginBadge(key string) string {
	origin := c.GetOrigin(key)
	switch origin {
	case OriginEnv:
		return "[env override]"
	case OriginWorkspace:
		if key == "server.encryption_key" && c.Server != nil && c.Server.EncryptionKey == "" {
			return "[workspace opt-out]"
		}
		return "[workspace override]"
	case OriginAnchor:
		return "[workspace anchor]"
	case OriginGlobal:
		return "[global preference]"
	default:
		return "[default]"
	}
}

// RecordOrigin updates the provenance of a configuration key after an interactive update.
func (c *Config) RecordOrigin(key string, isProjectSetting bool) {
	if c == nil {
		return
	}
	if c.Origins == nil {
		c.Origins = make(map[string]string)
	}
	if isProjectSetting && c.WorkspaceRoot != "" {
		c.Origins[key] = OriginWorkspace
	} else {
		c.Origins[key] = OriginGlobal
	}
}

// SaveScoped persists configuration changes based on whether a workspace is active and setting scope.
// Project settings (model, provider, options, sandbox, worktree) in an active workspace write to .please/config.json.
// Operator personal settings (bell, pacing, encryption_key) or settings changed in global mode write to ~/.please/config.json.
func (c *Config) SaveScoped(isProjectSetting bool) (savedPath string, err error) {
	if c.ReadOnly {
		return "", fmt.Errorf("cannot save configuration: running in read-only mode")
	}

	if isProjectSetting && c.WorkspaceRoot != "" {
		if err := c.SaveWorkspace(c.WorkspaceRoot); err != nil {
			return "", err
		}
		if c.Origins == nil {
			c.Origins = make(map[string]string)
		}
		return filepath.Join(c.WorkspaceRoot, ".please", "config.json"), nil
	}

	// Saving client/operator preference or in global mode
	if c.WorkspaceRoot != "" {
		// When inside an active workspace, do NOT clobber global server settings (model, vault_path, etc.)
		globalCfg, err := loadGlobalConfig()
		if err != nil {
			return "", fmt.Errorf("failed to load global config: %w", err)
		}
		if c.Client != nil {
			if globalCfg.Client == nil {
				globalCfg.Client = defaultClientConfig()
			}
			if c.Client.NaturalPacing != nil {
				globalCfg.Client.NaturalPacing = c.Client.NaturalPacing
			}
			if c.Client.BellOnTurnComplete != nil {
				globalCfg.Client.BellOnTurnComplete = c.Client.BellOnTurnComplete
			}
			if c.Client.RemoteURL != "" {
				globalCfg.Client.RemoteURL = c.Client.RemoteURL
			}
		}
		if c.Server != nil && c.Origins != nil && c.Origins["server.encryption_key"] == OriginGlobal {
			if globalCfg.Server == nil {
				globalCfg.Server = defaultServerConfig()
			}
			globalCfg.Server.EncryptionKey = c.Server.EncryptionKey
		}
		if err := globalCfg.Save(); err != nil {
			return "", err
		}
		appDir, _ := GetConfigDir()
		return filepath.Join(appDir, "config.json"), nil
	}

	if err := c.Save(); err != nil {
		return "", err
	}
	appDir, _ := GetConfigDir()
	return filepath.Join(appDir, "config.json"), nil
}

// SupportsVision returns whether the configured model supports vision/multimodal capabilities
func (c *Config) SupportsVision() bool {
	if c == nil || c.Server == nil {
		return false
	}
	if c.Server.Provider == "openai" {
		return true
	}
	modelLower := strings.ToLower(c.Server.Model)
	keywords := []string{"vision", "llava", "pixtral", "minicpm", "mplug", "bakllava", "llama3.2-vision", "llama-3.2-vision", "llama3-vision", "gemma4"}
	for _, kw := range keywords {
		if strings.Contains(modelLower, kw) {
			return true
		}
	}
	return false
}

// EnableNaturalPacing returns whether natural reading pacing is enabled on ClientConfig.
func (cl *ClientConfig) EnableNaturalPacing() bool {
	if cl == nil || cl.NaturalPacing == nil {
		return true
	}
	return *cl.NaturalPacing
}

// EnableNaturalPacing returns whether natural reading pacing is enabled across the configuration.
func (c *Config) EnableNaturalPacing() bool {
	if c == nil || c.Client == nil {
		return true
	}
	return c.Client.EnableNaturalPacing()
}

// IsPacingEnabled is a backward-compatible alias for EnableNaturalPacing.
func (c *Config) IsPacingEnabled() bool {
	return c.EnableNaturalPacing()
}

// EnableBellOnTurnComplete returns whether terminal bell cues on turn completion are enabled on ClientConfig.
// It prioritizes environment variables NO_BELL=1 and PLEASE_BELL=0 to enforce silence in scripts/CI.
// Otherwise, it defaults to true unless explicitly configured as false.
func (cl *ClientConfig) EnableBellOnTurnComplete() bool {
	if os.Getenv("NO_BELL") == "1" || os.Getenv("PLEASE_BELL") == "0" {
		return false
	}
	if cl == nil {
		return true
	}
	if cl.BellOnTurnComplete != nil {
		return *cl.BellOnTurnComplete
	}
	if cl.Bell != nil {
		return *cl.Bell
	}
	return true
}

// EnableBellOnTurnComplete returns whether terminal bell cues on turn completion are enabled across the configuration.
func (c *Config) EnableBellOnTurnComplete() bool {
	if os.Getenv("NO_BELL") == "1" || os.Getenv("PLEASE_BELL") == "0" {
		return false
	}
	if c == nil || c.Client == nil {
		return true
	}
	return c.Client.EnableBellOnTurnComplete()
}

// DefaultSessionName is the default session identifier used when no session is explicitly specified.
const DefaultSessionName = "main"

// GetSession returns the configured session name on ClientConfig, defaulting to "main".
func (cl *ClientConfig) GetSession() string {
	if cl == nil || cl.Session == "" {
		return DefaultSessionName
	}
	return cl.Session
}

// GetSession returns the configured session name across the configuration, defaulting to "main".
func (c *Config) GetSession() string {
	if c == nil || c.Client == nil {
		return DefaultSessionName
	}
	return c.Client.GetSession()
}

// GetMaxToolDepth returns the configured maximum multi-turn tool depth, or 15 by default.
func (s *ServerConfig) GetMaxToolDepth() int {
	if s != nil && s.MaxToolDepth != nil && *s.MaxToolDepth > 0 {
		return *s.MaxToolDepth
	}
	return 15
}

// GetMaxToolDepth returns the configured maximum multi-turn tool depth, or 15 by default.
func (c *Config) GetMaxToolDepth() int {
	if c != nil && c.Server != nil {
		return c.Server.GetMaxToolDepth()
	}
	return 15
}

// GetSandboxPolicy returns the active sandbox policy ("strict", "standard", "permissive").
// Defaults to SandboxPolicyStandard if not configured.
func (s *ServerConfig) GetSandboxPolicy() string {
	if s == nil || s.SandboxPolicy == "" {
		return tools.SandboxPolicyStandard
	}
	return s.SandboxPolicy
}

// GetSandboxPolicy returns the active sandbox policy from ServerConfig, defaulting to standard.
func (c *Config) GetSandboxPolicy() string {
	if c != nil && c.Server != nil {
		return c.Server.GetSandboxPolicy()
	}
	return tools.SandboxPolicyStandard
}

// EnableSignatSteering returns whether turn signature (signat) emoji steering is enabled.
// Defaults to false (clean, distraction-free prompts).
func (s *ServerConfig) EnableSignatSteering() bool {
	if s == nil || s.SignatSteering == nil {
		return false
	}
	return *s.SignatSteering
}

// EnableSignatSteering returns whether turn signature (signat) emoji steering is enabled from ServerConfig.
func (c *Config) EnableSignatSteering() bool {
	if c != nil && c.Server != nil {
		return c.Server.EnableSignatSteering()
	}
	return false
}

// EnableAmbientTelemetry returns whether ambient environmental telemetry is enabled.
// Defaults to false (clean, un-bumpered human turns).
func (s *ServerConfig) EnableAmbientTelemetry() bool {
	if s == nil || s.AmbientTelemetry == nil {
		return false
	}
	return *s.AmbientTelemetry
}

// EnableAmbientTelemetry returns whether ambient environmental telemetry is enabled from ServerConfig.
func (c *Config) EnableAmbientTelemetry() bool {
	if c != nil && c.Server != nil {
		return c.Server.EnableAmbientTelemetry()
	}
	return false
}

// EnableWorktreeIsolation returns whether Git worktree isolation is enabled.
// Defaults to false (opt-in).
func (s *ServerConfig) EnableWorktreeIsolation() bool {
	if s == nil || s.WorktreeIsolation == nil {
		return false
	}
	return *s.WorktreeIsolation
}

// EnableWorktreeIsolation returns whether Git worktree isolation is enabled from ServerConfig.
func (c *Config) EnableWorktreeIsolation() bool {
	if c != nil && c.Server != nil {
		return c.Server.EnableWorktreeIsolation()
	}
	return false
}

var (
	testDirOnce sync.Once
	testAutoDir string
)

func isTestEnvironment() bool {
	return flag.Lookup("test.v") != nil || strings.HasSuffix(os.Args[0], ".test")
}

func getTestFallbackDir() string {
	testDirOnce.Do(func() {
		dir, err := os.MkdirTemp("", "please-test-fallback-*")
		if err == nil {
			testAutoDir = dir
		} else {
			testAutoDir = filepath.Join(os.TempDir(), "please-test-fallback")
			_ = os.MkdirAll(testAutoDir, 0755)
		}
	})
	return testAutoDir
}

// GetGlobalPleaseDir returns the universal user-level Please directory (~/.please).
// It respects PLEASE_GLOBAL_DIR and PLEASE_CONFIG_DIR overrides and isolates test environments.
func GetGlobalPleaseDir() (string, error) {
	if dir := os.Getenv("PLEASE_GLOBAL_DIR"); dir != "" {
		_ = os.MkdirAll(dir, 0755)
		return dir, nil
	}
	if dir := os.Getenv("PLEASE_CONFIG_DIR"); dir != "" {
		_ = os.MkdirAll(dir, 0755)
		return dir, nil
	}
	if isTestEnvironment() {
		return getTestFallbackDir(), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user home directory: %w", err)
	}
	globalDir := filepath.Join(home, ".please")
	_ = os.MkdirAll(globalDir, 0755)

	// Transparent Migration from legacy UserConfigDir() (e.g. ~/Library/Application Support/please/config.json)
	migrateLegacyConfig(globalDir)

	return globalDir, nil
}

// migrateLegacyConfig copies existing configuration from the OS-specific application support
// directory to ~/.please/config.json if ~/.please/config.json does not yet exist.
func migrateLegacyConfig(globalDir string) {
	newConfigPath := filepath.Join(globalDir, "config.json")
	if _, err := os.Stat(newConfigPath); err == nil {
		return
	}

	legacyConfigDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	oldConfigPath := filepath.Join(legacyConfigDir, "please", "config.json")
	oldData, err := os.ReadFile(oldConfigPath)
	if err != nil || len(oldData) == 0 {
		return
	}

	_ = os.MkdirAll(globalDir, 0755)
	_ = os.WriteFile(newConfigPath, oldData, 0644)
}

// FindWorkspaceRoot inspects startDir (defaulting to current working directory).
// It does NOT crawl up or down directory hierarchies.
// Returns the directory path and whether a workspace anchor (.please) or repo (.git) exists in startDir.
func FindWorkspaceRoot(startDir ...string) (string, bool) {
	start := "."
	if len(startDir) > 0 && startDir[0] != "" {
		start = startDir[0]
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		abs = start
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	globalDir, _ := GetGlobalPleaseDir()
	if resolvedGlobal, err := filepath.EvalSymlinks(globalDir); err == nil {
		globalDir = resolvedGlobal
	}

	// 1. Check if startDir contains a .please directory (and is not global ~/.please)
	pleasePath := filepath.Join(abs, ".please")
	if pleasePath != globalDir {
		if fi, err := os.Stat(pleasePath); err == nil && fi.IsDir() {
			return abs, true
		}
	}

	// 2. Check if startDir is a git repository boundary
	gitPath := filepath.Join(abs, ".git")
	if _, err := os.Stat(gitPath); err == nil {
		return abs, true
	}

	return abs, false
}

// GetWorkspacePleaseDir returns the path to <target_dir>/.please if it exists in startDir.
// It does NOT crawl up or down parent/child directories.
func GetWorkspacePleaseDir(startDir ...string) (string, bool) {
	if ws := os.Getenv("PLEASE_WORKSPACE_DIR"); ws != "" {
		return ws, true
	}
	// If PLEASE_CONFIG_DIR is set (e.g. unit test isolation), disable ambient workspace discovery
	// unless an explicit start directory was provided.
	if os.Getenv("PLEASE_CONFIG_DIR") != "" && (len(startDir) == 0 || startDir[0] == "") {
		return "", false
	}

	start := "."
	if len(startDir) > 0 && startDir[0] != "" {
		start = startDir[0]
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		abs = start
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	globalDir, _ := GetGlobalPleaseDir()
	if resolvedGlobal, err := filepath.EvalSymlinks(globalDir); err == nil {
		globalDir = resolvedGlobal
	}

	pleasePath := filepath.Join(abs, ".please")
	if pleasePath != globalDir {
		if fi, err := os.Stat(pleasePath); err == nil && fi.IsDir() {
			return pleasePath, true
		}
	}

	return "", false
}

// GetConfigDir returns the directory where the global configuration file is stored.
func GetConfigDir() (string, error) {
	return GetGlobalPleaseDir()
}

// migrateConfig inspects raw JSON and converts legacy v1 flat configs to modern v2 schema
func migrateConfig(data []byte) (*Config, bool, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// If "server" block is present, it's already v2 schema
	if _, hasServer := raw["server"]; hasServer {
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, false, fmt.Errorf("failed to parse v2 config: %w", err)
		}
		if cfg.Server == nil {
			cfg.Server = defaultServerConfig()
		}
		if cfg.Client == nil {
			cfg.Client = defaultClientConfig()
		}
		if cfg.Version == 0 {
			cfg.Version = CurrentConfigVersion
		}
		if cfg.Server != nil {
			if cfg.Server.VaultPath == "" && cfg.Server.Vault != "" {
				cfg.Server.VaultPath = cfg.Server.Vault
			}
			if cfg.Server.WorkspaceDir == "" && cfg.Server.Workspace != "" {
				cfg.Server.WorkspaceDir = cfg.Server.Workspace
			}
		}
		return &cfg, false, nil
	}

	// Legacy v1 schema detected: migrate to v2
	var v1 legacyV1Config
	if err := json.Unmarshal(data, &v1); err != nil {
		return nil, false, fmt.Errorf("failed to parse legacy config: %w", err)
	}

	storageType := v1.StorageType
	if storageType == "" {
		storageType = "sqlite"
	}
	provider := v1.Provider
	if provider == "" {
		provider = "ollama"
	}
	model := v1.Model
	if model == "" {
		model = "gemma4:e4b"
	}
	endpoint := v1.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434/api/chat"
	}
	vaultPath := v1.VaultPath
	if vaultPath == "" {
		vaultPath = v1.Vault
	}
	if vaultPath == "" {
		if v, ok := raw["vault"].(string); ok && v != "" {
			vaultPath = v
		}
	}
	if vaultPath == "" {
		home, _ := os.UserHomeDir()
		vaultPath = filepath.Join(home, ".local", "share", "please", "vault.db")
	}

	workspaceDir := v1.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = v1.Workspace
	}
	if workspaceDir == "" {
		if w, ok := raw["workspace"].(string); ok && w != "" {
			workspaceDir = w
		}
	}

	pacing := true
	if v1.NaturalPacing != nil {
		pacing = *v1.NaturalPacing
	}

	bell := true
	if v1.BellOnTurnComplete != nil {
		bell = *v1.BellOnTurnComplete
	} else if v1.Bell != nil {
		bell = *v1.Bell
	} else if b, ok := raw["bell_on_turn_complete"].(bool); ok {
		bell = b
	} else if b, ok := raw["bell"].(bool); ok {
		bell = b
	}

	cfg := &Config{
		Version: CurrentConfigVersion,
		Mode:    "standalone",
		Server: &ServerConfig{
			Host:             "127.0.0.1",
			Port:             8080,
			Provider:         provider,
			Model:            model,
			Endpoint:         endpoint,
			APIKey:           v1.APIKey,
			VaultPath:        vaultPath,
			StorageType:      storageType,
			EncryptionKey:    v1.EncryptionKey,
			WorkspaceDir:     workspaceDir,
			AuthToken:        v1.AuthToken,
			TLSCertFile:      v1.TLSCertFile,
			TLSKeyFile:       v1.TLSKeyFile,
			SandboxPolicy:    v1.SandboxPolicy,
			SignatSteering:   v1.SignatSteering,
			AmbientTelemetry: v1.AmbientTelemetry,
			Options:          v1.Options,
		},
		Client: &ClientConfig{
			RemoteURL:          "http://127.0.0.1:8080",
			AuthToken:          v1.AuthToken,
			NaturalPacing:      &pacing,
			BellOnTurnComplete: &bell,
		},
	}

	return cfg, true, nil
}

// LoadConfigFile attempts to load and parse configuration from a specific file path,
// automatically migrating older or flat schemas (like livefire.json) to the modern v2 format.
func LoadConfigFile(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg, _, err := migrateConfig(data)
	if err != nil {
		return nil, err
	}
	cfg.ReadOnly = true
	return cfg, nil
}

// LoadConfig loads the active configuration following the ADR 016 discovery ladder:
// 1. Workspace-local anchor (.please/config.json if in an initialized workspace)
// 2. Global anchor (~/.please/config.json)
// 3. Built-in defaults
func LoadConfig() (*Config, error) {
	// 1. Check for workspace-local anchor
	if wsPleaseDir, ok := GetWorkspacePleaseDir(); ok {
		wsRoot := filepath.Dir(wsPleaseDir)
		wsConfigFile := filepath.Join(wsPleaseDir, "config.json")
		wsVaultFile := filepath.Join(wsPleaseDir, "vault.db")

		if _, err := os.Stat(wsConfigFile); err == nil {
			// Load global config as baseline
			globalCfg, _ := loadGlobalConfigQuietly()

			// Load workspace config
			wsCfg, err := LoadConfigFile(wsConfigFile)
			if err == nil {
				merged := mergeConfigs(globalCfg, wsCfg)
				if merged.Server == nil {
					merged.Server = defaultServerConfig()
				}
				// Bind workspace anchors
				merged.Server.VaultPath = wsVaultFile
				merged.Server.WorkspaceDir = wsRoot
				merged.WorkspaceRoot = wsRoot
				if merged.Origins == nil {
					merged.Origins = make(map[string]string)
				}
				merged.Origins["server.vault_path"] = "workspace anchor"
				merged.Origins["server.workspace_dir"] = "workspace anchor"
				merged.ReadOnly = false
				applyEnvironmentOverrides(merged)
				return merged, nil
			}
		} else {
			// .please/ directory exists without config.json: use global config and bind vault to .please/vault.db
			cfg, err := loadGlobalConfig()
			if err == nil {
				populateGlobalOrigins(cfg)
				if cfg.Server == nil {
					cfg.Server = defaultServerConfig()
				}
				cfg.Server.VaultPath = wsVaultFile
				cfg.Server.WorkspaceDir = wsRoot
				cfg.WorkspaceRoot = wsRoot
				if cfg.Origins == nil {
					cfg.Origins = make(map[string]string)
				}
				cfg.Origins["server.vault_path"] = "workspace anchor"
				cfg.Origins["server.workspace_dir"] = "workspace anchor"
				applyEnvironmentOverrides(cfg)
				return cfg, nil
			}
		}
	}

	// 2. Global anchor (~/.please/config.json)
	cfg, err := loadGlobalConfig()
	if err == nil {
		populateGlobalOrigins(cfg)
		applyEnvironmentOverrides(cfg)
	}
	return cfg, err
}

func applyEnvironmentOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if envKey := os.Getenv("PLEASE_ENCRYPTION_KEY"); envKey != "" {
		if cfg.Server == nil {
			cfg.Server = defaultServerConfig()
		}
		if cfg.Origins == nil {
			cfg.Origins = make(map[string]string)
		}
		if envKey == "none" || envKey == "disabled" || envKey == "clear" || envKey == "off" || envKey == "0" {
			cfg.Server.EncryptionKey = ""
		} else {
			cfg.Server.EncryptionKey = envKey
		}
		cfg.Origins["server.encryption_key"] = OriginEnv
	}
}

func populateGlobalOrigins(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Origins = make(map[string]string)
	setGlobalOrDef := func(key string, present bool) {
		if present {
			cfg.Origins[key] = "global"
		} else {
			cfg.Origins[key] = "default"
		}
	}

	srv := cfg.Server != nil
	setGlobalOrDef("server.model", srv && cfg.Server.Model != "")
	setGlobalOrDef("server.provider", srv && cfg.Server.Provider != "")
	setGlobalOrDef("server.endpoint", srv && cfg.Server.Endpoint != "")
	setGlobalOrDef("server.vault_path", srv && cfg.Server.VaultPath != "")
	setGlobalOrDef("server.workspace_dir", srv && cfg.Server.WorkspaceDir != "")
	setGlobalOrDef("server.encryption_key", srv && cfg.Server.EncryptionKey != "")
	setGlobalOrDef("server.sandbox_policy", srv && cfg.Server.SandboxPolicy != "")
	setGlobalOrDef("server.worktree_isolation", srv && cfg.Server.WorktreeIsolation != nil)
	setGlobalOrDef("server.signat_steering", srv && cfg.Server.SignatSteering != nil)
	setGlobalOrDef("server.ambient_telemetry", srv && cfg.Server.AmbientTelemetry != nil)
	setGlobalOrDef("server.options", srv && cfg.Server.Options != nil)

	cli := cfg.Client != nil
	setGlobalOrDef("client.natural_pacing", cli && cfg.Client.NaturalPacing != nil)
	setGlobalOrDef("client.bell_on_turn_complete", cli && cfg.Client.BellOnTurnComplete != nil)
}

func loadGlobalConfig() (*Config, error) {
	appDir, err := GetGlobalPleaseDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(appDir, "config.json")

	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create global please directory: %w", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			if err := cfg.Save(); err != nil {
				return nil, fmt.Errorf("could not save default config: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg, migrated, err := migrateConfig(data)
	if err != nil {
		return nil, err
	}

	if migrated {
		_ = cfg.Save()
	}

	return cfg, nil
}

func loadGlobalConfigQuietly() (*Config, error) {
	appDir, err := GetGlobalPleaseDir()
	if err != nil {
		return defaultConfig(), nil
	}
	configPath := filepath.Join(appDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return defaultConfig(), nil
	}
	cfg, _, err := migrateConfig(data)
	if err != nil {
		return defaultConfig(), nil
	}
	return cfg, nil
}

// mergeConfigs merges workspace override settings on top of base global configuration.
func mergeConfigs(base, override *Config) *Config {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	merged := *base

	if override.Mode != "" {
		merged.Mode = override.Mode
	}

	if override.Server != nil {
		if merged.Server == nil {
			merged.Server = override.Server
		} else {
			s := *merged.Server
			if override.Server.Host != "" {
				s.Host = override.Server.Host
			}
			if override.Server.Port != 0 {
				s.Port = override.Server.Port
			}
			if override.Server.Provider != "" {
				s.Provider = override.Server.Provider
			}
			if override.Server.Model != "" {
				s.Model = override.Server.Model
			}
			if override.Server.Endpoint != "" {
				s.Endpoint = override.Server.Endpoint
			}
			if override.Server.APIKey != "" {
				s.APIKey = override.Server.APIKey
			}
			if override.Server.VaultPath != "" {
				s.VaultPath = override.Server.VaultPath
			}
			if override.Server.StorageType != "" {
				s.StorageType = override.Server.StorageType
			}
			if override.Server.WorkspaceDir != "" {
				s.WorkspaceDir = override.Server.WorkspaceDir
			}
			if override.Server.EncryptionKey != "" {
				if override.Server.EncryptionKey == "none" || override.Server.EncryptionKey == "disabled" || override.Server.EncryptionKey == "clear" || override.Server.EncryptionKey == "off" {
					s.EncryptionKey = ""
				} else {
					s.EncryptionKey = override.Server.EncryptionKey
				}
			}
			if override.Server.SandboxPolicy != "" {
				s.SandboxPolicy = override.Server.SandboxPolicy
			}
			if override.Server.WorktreeIsolation != nil {
				s.WorktreeIsolation = override.Server.WorktreeIsolation
			}
			if override.Server.SignatSteering != nil {
				s.SignatSteering = override.Server.SignatSteering
			}
			if override.Server.AmbientTelemetry != nil {
				s.AmbientTelemetry = override.Server.AmbientTelemetry
			}
			if override.Server.Options != nil {
				s.Options = override.Server.Options
			}
			merged.Server = &s
		}
	}

	if override.Client != nil {
		if merged.Client == nil {
			merged.Client = override.Client
		} else {
			c := *merged.Client
			if override.Client.RemoteURL != "" {
				c.RemoteURL = override.Client.RemoteURL
			}
			if override.Client.NaturalPacing != nil {
				c.NaturalPacing = override.Client.NaturalPacing
			}
			if override.Client.BellOnTurnComplete != nil {
				c.BellOnTurnComplete = override.Client.BellOnTurnComplete
			}
			merged.Client = &c
		}
	}

	merged.Origins = make(map[string]string)
	if override.Server != nil {
		if override.Server.Model != "" {
			merged.Origins["server.model"] = "workspace"
		}
		if override.Server.Provider != "" {
			merged.Origins["server.provider"] = "workspace"
		}
		if override.Server.Endpoint != "" {
			merged.Origins["server.endpoint"] = "workspace"
		}
		if override.Server.EncryptionKey != "" {
			merged.Origins["server.encryption_key"] = "workspace"
		}
		if override.Server.SandboxPolicy != "" {
			merged.Origins["server.sandbox_policy"] = "workspace"
		}
		if override.Server.WorktreeIsolation != nil {
			merged.Origins["server.worktree_isolation"] = "workspace"
		}
		if override.Server.SignatSteering != nil {
			merged.Origins["server.signat_steering"] = "workspace"
		}
		if override.Server.AmbientTelemetry != nil {
			merged.Origins["server.ambient_telemetry"] = "workspace"
		}
		if override.Server.Options != nil {
			merged.Origins["server.options"] = "workspace"
		}
	}
	if override.Client != nil {
		if override.Client.NaturalPacing != nil {
			merged.Origins["client.natural_pacing"] = "workspace"
		}
		if override.Client.BellOnTurnComplete != nil {
			merged.Origins["client.bell_on_turn_complete"] = "workspace"
		}
	}

	checkBase := func(key string, present bool) {
		if _, ok := merged.Origins[key]; !ok {
			if present {
				merged.Origins[key] = "global"
			} else {
				merged.Origins[key] = "default"
			}
		}
	}
	baseSrv := base.Server != nil
	checkBase("server.model", baseSrv && base.Server.Model != "")
	checkBase("server.provider", baseSrv && base.Server.Provider != "")
	checkBase("server.endpoint", baseSrv && base.Server.Endpoint != "")
	checkBase("server.encryption_key", baseSrv && base.Server.EncryptionKey != "")
	checkBase("server.sandbox_policy", baseSrv && base.Server.SandboxPolicy != "")
	checkBase("server.worktree_isolation", baseSrv && base.Server.WorktreeIsolation != nil)
	checkBase("server.signat_steering", baseSrv && base.Server.SignatSteering != nil)
	checkBase("server.ambient_telemetry", baseSrv && base.Server.AmbientTelemetry != nil)
	checkBase("server.options", baseSrv && base.Server.Options != nil)

	baseCli := base.Client != nil
	checkBase("client.natural_pacing", baseCli && base.Client.NaturalPacing != nil)
	checkBase("client.bell_on_turn_complete", baseCli && base.Client.BellOnTurnComplete != nil)

	return &merged
}

// Save writes the current configuration to the user's config directory in v2 format
func (c *Config) Save() error {
	if c.ReadOnly {
		return fmt.Errorf("cannot save configuration: running in read-only mode (loaded via external file)")
	}

	appDir, err := GetConfigDir()
	if err != nil {
		return err
	}

	if c.Version == 0 {
		c.Version = CurrentConfigVersion
	}
	if c.Server == nil {
		c.Server = defaultServerConfig()
	}
	if c.Client == nil {
		c.Client = defaultClientConfig()
	}

	configPath := filepath.Join(appDir, "config.json")

	// Backup existing config if present and non-empty
	if existingData, err := os.ReadFile(configPath); err == nil && len(existingData) > 0 {
		backupPath := filepath.Join(appDir, "config.json.bak")
		_ = os.WriteFile(backupPath, existingData, 0600)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// SaveWorkspace writes the configuration to <workspace_root>/.please/config.json
func (c *Config) SaveWorkspace(workspaceDir ...string) error {
	var wsRoot string
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		wsRoot = workspaceDir[0]
	} else {
		wsRoot, _ = FindWorkspaceRoot()
	}
	if wsRoot == "" {
		var err error
		wsRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("could not determine workspace root: %w", err)
		}
	}
	pleaseDir := filepath.Join(wsRoot, ".please")
	if err := os.MkdirAll(pleaseDir, 0755); err != nil {
		return fmt.Errorf("could not create workspace .please directory: %w", err)
	}

	configPath := filepath.Join(pleaseDir, "config.json")

	// Never leak global secrets or non-workspace settings into workspace file
	toSave := *c
	if toSave.Server != nil {
		srvCopy := *toSave.Server
		if c.Origins == nil || c.Origins["server.encryption_key"] != OriginWorkspace {
			srvCopy.EncryptionKey = ""
		}
		toSave.Server = &srvCopy
	}
	if c.Origins == nil || (c.Origins["client.natural_pacing"] != OriginWorkspace && c.Origins["client.bell_on_turn_complete"] != OriginWorkspace) {
		toSave.Client = nil
	}

	data, err := json.MarshalIndent(&toSave, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func defaultServerConfig() *ServerConfig {
	globalDir, _ := GetGlobalPleaseDir()
	vaultPath := filepath.Join(globalDir, "vault.db")

	return &ServerConfig{
		Host:        "127.0.0.1",
		Port:        8080,
		Provider:    "ollama",
		Model:       "gemma4:e4b",
		Endpoint:    "http://localhost:11434/api/chat",
		VaultPath:   vaultPath,
		StorageType: "sqlite",
	}
}

func defaultClientConfig() *ClientConfig {
	pacing := true
	bell := true
	return &ClientConfig{
		RemoteURL:          "http://127.0.0.1:8080",
		NaturalPacing:      &pacing,
		BellOnTurnComplete: &bell,
	}
}

// NewDefaultConfig returns a freshly initialized default configuration
func NewDefaultConfig() *Config {
	return defaultConfig()
}

func defaultConfig() *Config {
	return &Config{
		Version: CurrentConfigVersion,
		Mode:    "standalone",
		Server:  defaultServerConfig(),
		Client:  defaultClientConfig(),
	}
}
