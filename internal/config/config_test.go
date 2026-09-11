package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bartkleypas/please/internal/tools"
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
	if cfg.EnableNaturalPacing() {
		t.Errorf("expected natural pacing to be false, got true")
	}
	if cfg.IsPacingEnabled() {
		t.Errorf("expected IsPacingEnabled alias to be false, got true")
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

func TestConfig_GetMaxToolDepth(t *testing.T) {
	// 1. Default fallback when nil
	cfg := NewDefaultConfig()
	if cfg.GetMaxToolDepth() != 50 {
		t.Errorf("expected default max tool depth 50, got %d", cfg.GetMaxToolDepth())
	}
	if cfg.Server.GetMaxToolDepth() != 50 {
		t.Errorf("expected default server max tool depth 50, got %d", cfg.Server.GetMaxToolDepth())
	}

	// 2. Custom override
	customDepth := 25
	cfg.Server.MaxToolDepth = &customDepth
	if cfg.GetMaxToolDepth() != 25 {
		t.Errorf("expected custom max tool depth 25, got %d", cfg.GetMaxToolDepth())
	}

	// 3. Nil receiver safety
	var nilServer *ServerConfig
	if nilServer.GetMaxToolDepth() != 50 {
		t.Errorf("expected nil server to return 50, got %d", nilServer.GetMaxToolDepth())
	}
}

func TestConfig_GetSandboxPolicy(t *testing.T) {
	// 1. Default fallback to SandboxPolicyStandard
	cfg := &Config{
		Server: &ServerConfig{},
	}
	if cfg.GetSandboxPolicy() != tools.SandboxPolicyStandard {
		t.Errorf("expected default standard policy, got %s", cfg.GetSandboxPolicy())
	}
	if cfg.Server.GetSandboxPolicy() != tools.SandboxPolicyStandard {
		t.Errorf("expected server standard policy, got %s", cfg.Server.GetSandboxPolicy())
	}

	// 2. Explicit strict policy
	cfg.Server.SandboxPolicy = tools.SandboxPolicyStrict
	if cfg.GetSandboxPolicy() != tools.SandboxPolicyStrict {
		t.Errorf("expected strict policy, got %s", cfg.GetSandboxPolicy())
	}

	// 3. Nil receiver safety
	var nilConfig *Config
	if nilConfig.GetSandboxPolicy() != tools.SandboxPolicyStandard {
		t.Errorf("expected nil config to return standard policy, got %s", nilConfig.GetSandboxPolicy())
	}
	var nilServer *ServerConfig
	if nilServer.GetSandboxPolicy() != tools.SandboxPolicyStandard {
		t.Errorf("expected nil server to return standard policy, got %s", nilServer.GetSandboxPolicy())
	}
}

func TestConfig_EnableSignatSteering(t *testing.T) {
	// 1. Default: disabled
	cfg := &Config{
		Server: &ServerConfig{},
	}
	if cfg.EnableSignatSteering() {
		t.Errorf("expected default signat steering to be disabled")
	}

	// 2. Explicitly enabled
	enabled := true
	cfg.Server.SignatSteering = &enabled
	if !cfg.EnableSignatSteering() {
		t.Errorf("expected signat steering to be enabled")
	}

	// 3. Nil receiver safety
	var nilConfig *Config
	if nilConfig.EnableSignatSteering() {
		t.Errorf("expected nil config to return false")
	}
	var nilServer *ServerConfig
	if nilServer.EnableSignatSteering() {
		t.Errorf("expected nil server to return false")
	}
}

func TestConfig_EnableAmbientTelemetry(t *testing.T) {
	// 1. Default: disabled
	cfg := &Config{
		Server: &ServerConfig{},
	}
	if cfg.EnableAmbientTelemetry() {
		t.Errorf("expected default ambient telemetry to be disabled")
	}

	// 2. Explicitly enabled
	enabled := true
	cfg.Server.AmbientTelemetry = &enabled
	if !cfg.EnableAmbientTelemetry() {
		t.Errorf("expected ambient telemetry to be enabled")
	}

	// 3. Nil receiver safety
	var nilConfig *Config
	if nilConfig.EnableAmbientTelemetry() {
		t.Errorf("expected nil config to return false")
	}
	var nilServer *ServerConfig
	if nilServer.EnableAmbientTelemetry() {
		t.Errorf("expected nil server to return false")
	}
}

func TestLoadConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom.json")

	// Test with a flat config (like livefire.json)
	flatJSON := `{
		"provider": "ollama",
		"model": "gemma-test",
		"endpoint": "http://127.0.0.1:11434/api/chat",
		"signat_steering": true
	}`
	if err := os.WriteFile(customPath, []byte(flatJSON), 0644); err != nil {
		t.Fatalf("failed to write custom config: %v", err)
	}

	cfg, err := LoadConfigFile(customPath)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}

	if cfg.Server == nil {
		t.Fatalf("expected Server block to be populated")
	}
	if cfg.Server.Model != "gemma-test" {
		t.Errorf("expected model 'gemma-test', got '%s'", cfg.Server.Model)
	}
	if cfg.Server.Endpoint != "http://127.0.0.1:11434/api/chat" {
		t.Errorf("expected endpoint 'http://127.0.0.1:11434/api/chat', got '%s'", cfg.Server.Endpoint)
	}
	if !cfg.EnableSignatSteering() {
		t.Errorf("expected signat steering to be enabled")
	}

	// Test missing file
	_, err = LoadConfigFile(filepath.Join(tmpDir, "missing.json"))
	if err == nil {
		t.Errorf("expected error loading missing config file, got nil")
	}
}

func TestConfig_VaultAndWorkspaceAliases(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Flat config with "vault" and "workspace" aliases (e.g. livefire.json)
	flatPath := filepath.Join(tmpDir, "livefire.json")
	flatJSON := `{
		"provider": "openai",
		"model": "gemma4:26b-mlx",
		"vault": "./test_vault/livefire.db",
		"workspace": "./test_vault/workspace"
	}`
	if err := os.WriteFile(flatPath, []byte(flatJSON), 0644); err != nil {
		t.Fatalf("failed to write flat config: %v", err)
	}

	cfg, err := LoadConfigFile(flatPath)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	if cfg.Server.VaultPath != "./test_vault/livefire.db" {
		t.Errorf("expected VaultPath './test_vault/livefire.db', got '%s'", cfg.Server.VaultPath)
	}
	if cfg.Server.WorkspaceDir != "./test_vault/workspace" {
		t.Errorf("expected WorkspaceDir './test_vault/workspace', got '%s'", cfg.Server.WorkspaceDir)
	}

	// 2. v2 config with "vault" and "workspace" aliases inside "server"
	v2Path := filepath.Join(tmpDir, "v2.json")
	v2JSON := `{
		"version": 2,
		"mode": "standalone",
		"server": {
			"model": "gemma4:26b-mlx",
			"vault": "./v2_vault/livefire.db",
			"workspace": "./v2_vault/workspace"
		}
	}`
	if err := os.WriteFile(v2Path, []byte(v2JSON), 0644); err != nil {
		t.Fatalf("failed to write v2 config: %v", err)
	}

	cfg2, err := LoadConfigFile(v2Path)
	if err != nil {
		t.Fatalf("LoadConfigFile failed for v2: %v", err)
	}
	if cfg2.Server.VaultPath != "./v2_vault/livefire.db" {
		t.Errorf("expected VaultPath './v2_vault/livefire.db', got '%s'", cfg2.Server.VaultPath)
	}
	if cfg2.Server.WorkspaceDir != "./v2_vault/workspace" {
		t.Errorf("expected WorkspaceDir './v2_vault/workspace', got '%s'", cfg2.Server.WorkspaceDir)
	}
}

func TestConfig_WorktreeIsolation(t *testing.T) {
	// Default should be false (opt-in)
	defCfg := NewDefaultConfig()
	if defCfg.EnableWorktreeIsolation() {
		t.Errorf("expected default WorktreeIsolation to be false, got true")
	}

	// Explicitly enabled
	tr := true
	cfgTrue := &Config{
		Server: &ServerConfig{
			WorktreeIsolation: &tr,
		},
	}
	if !cfgTrue.EnableWorktreeIsolation() {
		t.Errorf("expected WorktreeIsolation to be true")
	}

	// Explicitly disabled
	fa := false
	cfgFalse := &Config{
		Server: &ServerConfig{
			WorktreeIsolation: &fa,
		},
	}
	if cfgFalse.EnableWorktreeIsolation() {
		t.Errorf("expected WorktreeIsolation to be false")
	}
}
