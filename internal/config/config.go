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
	RemoteURL     string `json:"remote_url,omitempty"`
	AuthToken     string `json:"auth_token,omitempty"`
	CACertPath    string `json:"ca_cert_path,omitempty"`
	NaturalPacing *bool  `json:"natural_pacing,omitempty"`
	Session       string `json:"session,omitempty"`
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
	Provider         string        `json:"provider"`
	APIKey           string        `json:"api_key"`
	Model            string        `json:"model"`
	Endpoint         string        `json:"endpoint"`
	VaultPath        string        `json:"vault_path"`
	Vault            string        `json:"vault"`
	StorageType      string        `json:"storage_type"`
	EncryptionKey    string        `json:"encryption_key"`
	NaturalPacing    *bool         `json:"natural_pacing"`
	Options          *ModelOptions `json:"options"`
	WorkspaceDir     string        `json:"workspace_dir"`
	Workspace        string        `json:"workspace"`
	AuthToken        string        `json:"auth_token"`
	TLSCertFile      string        `json:"tls_cert_file"`
	TLSKeyFile       string        `json:"tls_key_file"`
	SandboxPolicy    string        `json:"sandbox_policy"`
	SignatSteering   *bool         `json:"signat_steering"`
	AmbientTelemetry *bool         `json:"ambient_telemetry"`
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

// GetConfigDir returns the directory where the configuration file is stored.
func GetConfigDir() (string, error) {
	if dir := os.Getenv("PLEASE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	if isTestEnvironment() {
		return getTestFallbackDir(), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user config directory: %w", err)
	}
	return filepath.Join(configDir, "please"), nil
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
			RemoteURL:     "http://127.0.0.1:8080",
			AuthToken:     v1.AuthToken,
			NaturalPacing: &pacing,
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

// LoadConfig attempts to load the config from the user's config directory,
// automatically migrating older schemas to the modern v2 namespaced format.
func LoadConfig() (*Config, error) {
	appDir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(appDir, "config.json")

	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create config directory: %w", err)
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

	// If migrated from older schema, persist clean v2 config to disk
	if migrated {
		_ = cfg.Save()
	}

	return cfg, nil
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

func defaultServerConfig() *ServerConfig {
	home, _ := os.UserHomeDir()
	vaultDir := filepath.Join(home, ".local", "share", "please")
	_ = os.MkdirAll(vaultDir, 0755)

	return &ServerConfig{
		Host:        "127.0.0.1",
		Port:        8080,
		Provider:    "ollama",
		Model:       "gemma4:e4b",
		Endpoint:    "http://localhost:11434/api/chat",
		VaultPath:   filepath.Join(vaultDir, "vault.db"),
		StorageType: "sqlite",
	}
}

func defaultClientConfig() *ClientConfig {
	pacing := true
	return &ClientConfig{
		RemoteURL:     "http://127.0.0.1:8080",
		NaturalPacing: &pacing,
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
