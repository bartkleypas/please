package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "please-engine-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	os.Exit(m.Run())
}

func TestEngineConfig_ReExportsAndAliases(t *testing.T) {
	cfg := NewDefaultConfig()
	if cfg == nil {
		t.Fatal("expected NewDefaultConfig() to return non-nil Config")
	}

	if cfg.Version != CurrentConfigVersion {
		t.Errorf("expected Version %d, got %d", CurrentConfigVersion, cfg.Version)
	}

	if cfg.Server == nil || cfg.Client == nil {
		t.Fatal("expected Server and Client to be populated")
	}

	// Verify method forwarding on aliased type
	if !cfg.EnableNaturalPacing() {
		t.Errorf("expected EnableNaturalPacing() to be true by default")
	}
	if !cfg.IsPacingEnabled() {
		t.Errorf("expected backward-compatible IsPacingEnabled() to be true by default")
	}
	if cfg.GetMaxToolDepth() != 15 {
		t.Errorf("expected default max tool depth 15, got %d", cfg.GetMaxToolDepth())
	}
	if cfg.EnableSignatSteering() {
		t.Errorf("expected EnableSignatSteering() to be false by default")
	}
	if cfg.EnableAmbientTelemetry() {
		t.Errorf("expected EnableAmbientTelemetry() to be false by default")
	}
	if !cfg.EnableBellOnTurnComplete() {
		t.Errorf("expected EnableBellOnTurnComplete() to be true by default")
	}

	// Verify GetConfigDir
	dir, err := GetConfigDir()
	if err != nil || dir == "" {
		t.Fatalf("expected valid config dir, got %s (err: %v)", dir, err)
	}
}

func TestEngineConfig_LoadConfigFile_Migration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "legacy_config.json")

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
			"temperature": 0.75
		}
	}`

	if err := os.WriteFile(configPath, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatalf("failed to load legacy config via LoadConfigFile: %v", err)
	}

	if cfg.Version != CurrentConfigVersion {
		t.Errorf("expected version %d, got %d", CurrentConfigVersion, cfg.Version)
	}
	if cfg.Server.Model != "mistral-nemo:12b" {
		t.Errorf("expected model 'mistral-nemo:12b', got %s", cfg.Server.Model)
	}
	if cfg.EnableNaturalPacing() {
		t.Errorf("expected EnableNaturalPacing() to be false")
	}
	if cfg.IsPacingEnabled() {
		t.Errorf("expected IsPacingEnabled() alias to be false")
	}
}
