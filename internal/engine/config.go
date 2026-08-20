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

// Config holds the application settings
type Config struct {
	Provider      string        `json:"provider"`
	APIKey        string        `json:"api_key,omitempty"`
	Model         string        `json:"model"`
	Endpoint      string        `json:"endpoint"`
	VaultPath     string        `json:"vault_path"`
	StorageType   string        `json:"storage_type"` // "jsonl" or "sqlite"
	EncryptionKey string        `json:"encryption_key,omitempty"`
	NaturalPacing *bool         `json:"natural_pacing,omitempty"`
	Options       *ModelOptions `json:"options,omitempty"`
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
// If PLEASE_CONFIG_DIR is set, it overrides the system user config directory.
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
// If it doesn't exist, it creates one with sensible defaults.
func LoadConfig() (*Config, error) {
	appDir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(appDir, "config.json")
	// fmt.Printf("DEBUG: Looking for config at: %s\n", configPath)

	// Ensure the directory exists
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create config directory: %w", err)
	}

	// Try to read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default config
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

	// Backward compatibility: default to jsonl if not specified
	if cfg.StorageType == "" {
		cfg.StorageType = "jsonl"
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

	configPath := filepath.Join(appDir, "config.json")

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func defaultConfig() *Config {
	// Default vault path: ~/.local/share/please/vault.jsonl (on Linux/macOS)
	home, _ := os.UserHomeDir()
	vaultDir := filepath.Join(home, ".local", "share", "please")
	// fmt.Printf("DEBUG: Creating default vault at: %s\n", vaultDir)

	// Ensure data directory exists
	_ = os.MkdirAll(vaultDir, 0755)

	pacing := true
	return &Config{
		Provider:      "ollama",
		Model:         "gemma4:e4b",
		Endpoint:      "http://localhost:11434/api/chat",
		VaultPath:     filepath.Join(vaultDir, "vault.db"),
		StorageType:   "sqlite",
		NaturalPacing: &pacing,
	}
}
