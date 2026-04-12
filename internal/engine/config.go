package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the application settings
type Config struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	VaultPath string `json:"vault_path"`
}

// LoadConfig attempts to load the config from the user's config directory.
// If it doesn't exist, it creates one with sensible defaults.
func LoadConfig() (*Config, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("could not determine user config directory: %w", err)
	}

	appDir := filepath.Join(configDir, "please")
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

	return &cfg, nil
}

// Save writes the current configuration to the user's config directory
func (c *Config) Save() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	appDir := filepath.Join(configDir, "please")
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

	return &Config{
		Model:     "llama3",
		Endpoint:  "http://localhost:11434/api/chat",
		VaultPath: filepath.Join(vaultDir, "vault.jsonl"),
	}
}
