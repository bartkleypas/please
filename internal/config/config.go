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
	Version  int           `json:"version"`
	Mode     string        `json:"mode,omitempty"` // "standalone", "server", "client"
	Server   *ServerConfig `json:"server"`
	Client   *ClientConfig `json:"client"`
	ReadOnly bool          `json:"-"` // Prevents persisting mutations to disk when loaded via external file
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
				// If workspace config did not explicitly set a vault_path, bind to .please/vault.db
				if merged.Server.VaultPath == "" || merged.Server.VaultPath == defaultServerConfig().VaultPath {
					merged.Server.VaultPath = wsVaultFile
				}
				merged.ReadOnly = false
				return merged, nil
			}
		} else {
			// .please/ directory exists without config.json: use global config and bind vault to .please/vault.db
			cfg, err := loadGlobalConfig()
			if err == nil {
				if cfg.Server == nil {
					cfg.Server = defaultServerConfig()
				}
				if cfg.Server.VaultPath == "" || cfg.Server.VaultPath == defaultServerConfig().VaultPath {
					cfg.Server.VaultPath = wsVaultFile
				}
				return cfg, nil
			}
		}
	}

	// 2. Global anchor (~/.please/config.json)
	return loadGlobalConfig()
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
	wsRoot, _ := FindWorkspaceRoot(workspaceDir...)
	pleaseDir := filepath.Join(wsRoot, ".please")
	if err := os.MkdirAll(pleaseDir, 0755); err != nil {
		return fmt.Errorf("could not create workspace .please directory: %w", err)
	}

	configPath := filepath.Join(pleaseDir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
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
