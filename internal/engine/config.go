package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CurrentConfigVersion is the current schema version for config.json
const CurrentConfigVersion = 2

// ModelOptions holds model inference and sampling parameters
type ModelOptions struct {
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	TopK          *int     `json:"top_k,omitempty"`
	NumCtx        *int     `json:"num_ctx,omitempty"`
	MaxTokens     *int     `json:"max_tokens,omitempty"`
	RepeatPenalty *float64 `json:"repeat_penalty,omitempty"`
}

// ServerConfig holds settings for running the engine daemon / standalone backend
type ServerConfig struct {
	Host          string        `json:"host,omitempty"`
	Port          int           `json:"port,omitempty"`
	Provider      string        `json:"provider,omitempty"`
	APIKey        string        `json:"api_key,omitempty"`
	Model         string        `json:"model,omitempty"`
	Endpoint      string        `json:"endpoint,omitempty"`
	VaultPath     string        `json:"vault_path,omitempty"`
	StorageType   string        `json:"storage_type,omitempty"` // "jsonl" or "sqlite"
	EncryptionKey string        `json:"encryption_key,omitempty"`
	WorkspaceDir  string        `json:"workspace_dir,omitempty"`
	AuthToken     string        `json:"auth_token,omitempty"`
	TLSCertFile   string        `json:"tls_cert_file,omitempty"`
	TLSKeyFile    string        `json:"tls_key_file,omitempty"`
	SandboxPolicy string        `json:"sandbox_policy,omitempty"` // "strict", "standard", "permissive"
	Options       *ModelOptions `json:"options,omitempty"`
}

// ClientConfig holds settings for connecting the TUI to a remote daemon
type ClientConfig struct {
	RemoteURL     string `json:"remote_url,omitempty"`
	AuthToken     string `json:"auth_token,omitempty"`
	CACertPath    string `json:"ca_cert_path,omitempty"`
	NaturalPacing *bool  `json:"natural_pacing,omitempty"`
}

// Config is the top-level configuration container (v2 schema)
type Config struct {
	Version int           `json:"version"`
	Mode    string        `json:"mode,omitempty"` // "standalone", "server", "client"
	Server  *ServerConfig `json:"server"`
	Client  *ClientConfig `json:"client"`
}

// legacyV1Config mirrors the flat v1 schema for migration
type legacyV1Config struct {
	Provider      string        `json:"provider"`
	APIKey        string        `json:"api_key"`
	Model         string        `json:"model"`
	Endpoint      string        `json:"endpoint"`
	VaultPath     string        `json:"vault_path"`
	StorageType   string        `json:"storage_type"`
	EncryptionKey string        `json:"encryption_key"`
	NaturalPacing *bool         `json:"natural_pacing"`
	Options       *ModelOptions `json:"options"`
	WorkspaceDir  string        `json:"workspace_dir"`
	AuthToken     string        `json:"auth_token"`
	TLSCertFile   string        `json:"tls_cert_file"`
	TLSKeyFile    string        `json:"tls_key_file"`
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
	keywords := []string{"vision", "llava", "pixtral", "minicpm", "mplug", "bakllava", "llama3.2-vision", "llama-3.2-vision", "llama3-vision"}
	for _, kw := range keywords {
		if strings.Contains(modelLower, kw) {
			return true
		}
	}
	return false
}

// IsPacingEnabled returns whether natural reading pacing is enabled
func (c *Config) IsPacingEnabled() bool {
	if c == nil || c.Client == nil || c.Client.NaturalPacing == nil {
		return true
	}
	return *c.Client.NaturalPacing
}

// GetConfigDir returns the directory where the configuration file is stored.
func GetConfigDir() (string, error) {
	if dir := os.Getenv("PLEASE_CONFIG_DIR"); dir != "" {
		return dir, nil
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
		home, _ := os.UserHomeDir()
		vaultPath = filepath.Join(home, ".local", "share", "please", "vault.db")
	}

	pacing := true
	if v1.NaturalPacing != nil {
		pacing = *v1.NaturalPacing
	}

	cfg := &Config{
		Version: CurrentConfigVersion,
		Mode:    "standalone",
		Server: &ServerConfig{
			Host:          "127.0.0.1",
			Port:          8080,
			Provider:      provider,
			Model:         model,
			Endpoint:      endpoint,
			APIKey:        v1.APIKey,
			VaultPath:     vaultPath,
			StorageType:   storageType,
			EncryptionKey: v1.EncryptionKey,
			WorkspaceDir:  v1.WorkspaceDir,
			AuthToken:     v1.AuthToken,
			TLSCertFile:   v1.TLSCertFile,
			TLSKeyFile:    v1.TLSKeyFile,
			Options:       v1.Options,
		},
		Client: &ClientConfig{
			RemoteURL:     "http://127.0.0.1:8080",
			AuthToken:     v1.AuthToken,
			NaturalPacing: &pacing,
		},
	}

	return cfg, true, nil
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
