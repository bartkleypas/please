package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModelOptions holds model inference and sampling parameters
type ModelOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	NumCtx      *int     `json:"num_ctx,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

// ServerConfig holds settings for running the engine daemon
type ServerConfig struct {
	Host          string        `json:"host,omitempty"`
	Port          int           `json:"port,omitempty"`
	Provider      string        `json:"provider,omitempty"`
	APIKey        string        `json:"api_key,omitempty"`
	Model         string        `json:"model,omitempty"`
	Endpoint      string        `json:"endpoint,omitempty"`
	VaultPath     string        `json:"vault_path,omitempty"`
	StorageType   string        `json:"storage_type,omitempty"`
	EncryptionKey string        `json:"encryption_key,omitempty"`
	WorkspaceDir  string        `json:"workspace_dir,omitempty"`
	AuthToken     string        `json:"auth_token,omitempty"`
	TLSCertFile   string        `json:"tls_cert_file,omitempty"`
	TLSKeyFile    string        `json:"tls_key_file,omitempty"`
	Options       *ModelOptions `json:"options,omitempty"`
}

// ClientConfig holds settings for connecting the TUI to a remote daemon
type ClientConfig struct {
	RemoteURL     string `json:"remote_url,omitempty"`
	AuthToken     string `json:"auth_token,omitempty"`
	CACertPath    string `json:"ca_cert_path,omitempty"`
	NaturalPacing *bool  `json:"natural_pacing,omitempty"`
}

// Config holds the application settings, supporting both namespaced and flat formats
type Config struct {
	Mode string `json:"mode,omitempty"` // "standalone", "server", "client"

	Server *ServerConfig `json:"server,omitempty"`
	Client *ClientConfig `json:"client,omitempty"`

	// Flat fields for direct access and backwards compatibility
	Provider      string        `json:"provider,omitempty"`
	APIKey        string        `json:"api_key,omitempty"`
	Model         string        `json:"model,omitempty"`
	Endpoint      string        `json:"endpoint,omitempty"`
	VaultPath     string        `json:"vault_path,omitempty"`
	StorageType   string        `json:"storage_type,omitempty"` // "jsonl" or "sqlite"
	EncryptionKey string        `json:"encryption_key,omitempty"`
	NaturalPacing *bool         `json:"natural_pacing,omitempty"`
	Options       *ModelOptions `json:"options,omitempty"`
	WorkspaceDir  string        `json:"workspace_dir,omitempty"`
	AuthToken     string        `json:"auth_token,omitempty"`
	TLSCertFile   string        `json:"tls_cert_file,omitempty"`
	TLSKeyFile    string        `json:"tls_key_file,omitempty"`
}

// SyncOnLoad is called after unmarshaling JSON to populate namespaces or flat fields
func (c *Config) SyncOnLoad() {
	if c.Server == nil {
		c.Server = &ServerConfig{}
	}
	if c.Client == nil {
		c.Client = &ClientConfig{}
	}

	// If Server block was provided in JSON, copy Server -> flat
	if c.Server.Provider != "" && c.Provider == "" {
		c.Provider = c.Server.Provider
	}
	if c.Server.Model != "" && c.Model == "" {
		c.Model = c.Server.Model
	}
	if c.Server.Endpoint != "" && c.Endpoint == "" {
		c.Endpoint = c.Server.Endpoint
	}
	if c.Server.APIKey != "" && c.APIKey == "" {
		c.APIKey = c.Server.APIKey
	}
	if c.Server.VaultPath != "" && c.VaultPath == "" {
		c.VaultPath = c.Server.VaultPath
	}
	if c.Server.StorageType != "" && c.StorageType == "" {
		c.StorageType = c.Server.StorageType
	}
	if c.Server.EncryptionKey != "" && c.EncryptionKey == "" {
		c.EncryptionKey = c.Server.EncryptionKey
	}
	if c.Server.WorkspaceDir != "" && c.WorkspaceDir == "" {
		c.WorkspaceDir = c.Server.WorkspaceDir
	}
	if c.Server.AuthToken != "" && c.AuthToken == "" {
		c.AuthToken = c.Server.AuthToken
	}
	if c.Server.TLSCertFile != "" && c.TLSCertFile == "" {
		c.TLSCertFile = c.Server.TLSCertFile
	}
	if c.Server.TLSKeyFile != "" && c.TLSKeyFile == "" {
		c.TLSKeyFile = c.Server.TLSKeyFile
	}
	if c.Server.Options != nil && c.Options == nil {
		c.Options = c.Server.Options
	}

	// If flat fields were provided in JSON, copy flat -> Server
	if c.Provider != "" && c.Server.Provider == "" {
		c.Server.Provider = c.Provider
	}
	if c.Model != "" && c.Server.Model == "" {
		c.Server.Model = c.Model
	}
	if c.Endpoint != "" && c.Server.Endpoint == "" {
		c.Server.Endpoint = c.Endpoint
	}
	if c.APIKey != "" && c.Server.APIKey == "" {
		c.Server.APIKey = c.APIKey
	}
	if c.VaultPath != "" && c.Server.VaultPath == "" {
		c.Server.VaultPath = c.VaultPath
	}
	if c.StorageType != "" && c.Server.StorageType == "" {
		c.Server.StorageType = c.StorageType
	}
	if c.EncryptionKey != "" && c.Server.EncryptionKey == "" {
		c.Server.EncryptionKey = c.EncryptionKey
	}
	if c.WorkspaceDir != "" && c.Server.WorkspaceDir == "" {
		c.Server.WorkspaceDir = c.WorkspaceDir
	}
	if c.AuthToken != "" && c.Server.AuthToken == "" {
		c.Server.AuthToken = c.AuthToken
	}
	if c.TLSCertFile != "" && c.Server.TLSCertFile == "" {
		c.Server.TLSCertFile = c.TLSCertFile
	}
	if c.TLSKeyFile != "" && c.Server.TLSKeyFile == "" {
		c.Server.TLSKeyFile = c.TLSKeyFile
	}
	if c.Options != nil && c.Server.Options == nil {
		c.Server.Options = c.Options
	}

	if c.Client.NaturalPacing != nil && c.NaturalPacing == nil {
		c.NaturalPacing = c.Client.NaturalPacing
	}
	if c.NaturalPacing != nil && c.Client.NaturalPacing == nil {
		c.Client.NaturalPacing = c.NaturalPacing
	}
	if c.Client.AuthToken != "" && c.AuthToken == "" {
		c.AuthToken = c.Client.AuthToken
	}
}

// SyncNamespaces updates Server and Client sub-structs from flat fields prior to saving
func (c *Config) SyncNamespaces() {
	if c.Server == nil {
		c.Server = &ServerConfig{}
	}
	if c.Client == nil {
		c.Client = &ClientConfig{}
	}

	c.Server.Provider = c.Provider
	c.Server.Model = c.Model
	c.Server.Endpoint = c.Endpoint
	c.Server.APIKey = c.APIKey
	c.Server.VaultPath = c.VaultPath
	c.Server.StorageType = c.StorageType
	c.Server.EncryptionKey = c.EncryptionKey
	c.Server.WorkspaceDir = c.WorkspaceDir
	c.Server.AuthToken = c.AuthToken
	c.Server.TLSCertFile = c.TLSCertFile
	c.Server.TLSKeyFile = c.TLSKeyFile
	c.Server.Options = c.Options

	c.Client.NaturalPacing = c.NaturalPacing
	if c.Client.AuthToken == "" && c.AuthToken != "" {
		c.Client.AuthToken = c.AuthToken
	}
}

// GetWorkspaceDir returns the resolved absolute workspace directory.
func (c *Config) GetWorkspaceDir() string {
	if c == nil || c.WorkspaceDir == "" {
		return "."
	}
	dir := os.ExpandEnv(c.WorkspaceDir)
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
	if c.Provider == "openai" {
		return true
	}
	modelLower := strings.ToLower(c.Model)
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
	return c.NaturalPacing == nil || *c.NaturalPacing
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

// LoadConfig attempts to load the config from the user's config directory.
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

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	cfg.SyncOnLoad()

	if cfg.StorageType == "" {
		cfg.StorageType = "sqlite"
	}
	if cfg.Provider == "" {
		cfg.Provider = "ollama"
	}
	if cfg.NaturalPacing == nil {
		pacing := true
		cfg.NaturalPacing = &pacing
	}

	return &cfg, nil
}

// Save writes the current configuration to the user's config directory
func (c *Config) Save() error {
	appDir, err := GetConfigDir()
	if err != nil {
		return err
	}

	c.SyncNamespaces()

	configPath := filepath.Join(appDir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func defaultConfig() *Config {
	home, _ := os.UserHomeDir()
	vaultDir := filepath.Join(home, ".local", "share", "please")
	_ = os.MkdirAll(vaultDir, 0755)

	pacing := true
	cfg := &Config{
		Mode:          "standalone",
		Provider:      "ollama",
		Model:         "gemma4:e4b",
		Endpoint:      "http://localhost:11434/api/chat",
		VaultPath:     filepath.Join(vaultDir, "vault.db"),
		StorageType:   "sqlite",
		NaturalPacing: &pacing,
		Server: &ServerConfig{
			Host:        "127.0.0.1",
			Port:        8080,
			Provider:    "ollama",
			Model:       "gemma4:e4b",
			Endpoint:    "http://localhost:11434/api/chat",
			VaultPath:   filepath.Join(vaultDir, "vault.db"),
			StorageType: "sqlite",
		},
		Client: &ClientConfig{
			RemoteURL:     "http://127.0.0.1:8080",
			NaturalPacing: &pacing,
		},
	}
	return cfg
}
