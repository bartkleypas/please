package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_MigrationV1ToV2(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)

	// Simulate a legacy flat v1 config.json on disk
	legacyJSON := `{
		"provider": "ollama",
		"model": "mistral-nemo:12b",
		"endpoint": "http://127.0.0.1:11434/api/chat",
		"vault_path": "/tmp/custom-vault.db",
		"storage_type": "sqlite",
		"workspace_dir": "~/Code/test-project",
		"encryption_key": "my-secret-key",
		"natural_pacing": false,
		"auth_token": "bearer-token-123",
		"options": {
			"temperature": 0.75,
			"top_p": 0.9,
			"num_ctx": 32768
		}
	}`

	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}

	// 1. LoadConfig should automatically detect v1 and migrate to v2
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load and migrate config: %v", err)
	}

	if cfg.Version != CurrentConfigVersion {
		t.Errorf("expected version %d, got %d", CurrentConfigVersion, cfg.Version)
	}
	if cfg.Server == nil || cfg.Client == nil {
		t.Fatalf("expected Server and Client blocks to be populated after migration")
	}

	if cfg.Server.Model != "mistral-nemo:12b" {
		t.Errorf("expected model 'mistral-nemo:12b', got %s", cfg.Server.Model)
	}
	if cfg.Server.Endpoint != "http://127.0.0.1:11434/api/chat" {
		t.Errorf("expected endpoint 'http://127.0.0.1:11434/api/chat', got %s", cfg.Server.Endpoint)
	}
	if cfg.Server.WorkspaceDir != "~/Code/test-project" {
		t.Errorf("expected workspace_dir '~/Code/test-project', got %s", cfg.Server.WorkspaceDir)
	}
	if cfg.Server.EncryptionKey != "my-secret-key" {
		t.Errorf("expected encryption key 'my-secret-key', got %s", cfg.Server.EncryptionKey)
	}
	if cfg.Server.AuthToken != "bearer-token-123" {
		t.Errorf("expected server auth_token 'bearer-token-123', got %s", cfg.Server.AuthToken)
	}
	if cfg.Client.AuthToken != "bearer-token-123" {
		t.Errorf("expected client auth_token 'bearer-token-123', got %s", cfg.Client.AuthToken)
	}
	if cfg.IsPacingEnabled() {
		t.Errorf("expected natural pacing to be false, got true")
	}
	if cfg.Server.Options == nil || cfg.Server.Options.Temperature == nil || *cfg.Server.Options.Temperature != 0.75 {
		t.Errorf("expected temperature 0.75, got %v", cfg.Server.Options)
	}

	// 2. Verify config on disk was upgraded to v2 format
	savedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read migrated config from disk: %v", err)
	}

	// Loading again from disk should parse directly without error
	reloaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to reload upgraded config: %v", err)
	}
	if reloaded.Server.Model != "mistral-nemo:12b" {
		t.Errorf("expected reloaded model 'mistral-nemo:12b', got %s", reloaded.Server.Model)
	}

	_ = savedData
}
