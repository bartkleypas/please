package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/domain"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "please-config-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	os.Exit(m.Run())
}

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
	if cfg.GetMaxToolDepth() != 15 {
		t.Errorf("expected default max tool depth 15, got %d", cfg.GetMaxToolDepth())
	}
	if cfg.Server.GetMaxToolDepth() != 15 {
		t.Errorf("expected default server max tool depth 15, got %d", cfg.Server.GetMaxToolDepth())
	}

	// 2. Custom override
	customDepth := 25
	cfg.Server.MaxToolDepth = &customDepth
	if cfg.GetMaxToolDepth() != 25 {
		t.Errorf("expected custom max tool depth 25, got %d", cfg.GetMaxToolDepth())
	}

	// 3. Nil receiver safety
	var nilServer *ServerConfig
	if nilServer.GetMaxToolDepth() != 15 {
		t.Errorf("expected nil server to return 15, got %d", nilServer.GetMaxToolDepth())
	}
}

func TestConfig_GetSandboxPolicy(t *testing.T) {
	// 1. Default fallback to SandboxPolicyStandard
	cfg := &Config{
		Server: &ServerConfig{},
	}
	if cfg.GetSandboxPolicy() != string(domain.SandboxPolicyStandard) {
		t.Errorf("expected default standard policy, got %s", cfg.GetSandboxPolicy())
	}
	if cfg.Server.GetSandboxPolicy() != string(domain.SandboxPolicyStandard) {
		t.Errorf("expected server standard policy, got %s", cfg.Server.GetSandboxPolicy())
	}

	// 2. Explicit strict policy
	cfg.Server.SandboxPolicy = string(domain.SandboxPolicyStrict)
	if cfg.GetSandboxPolicy() != string(domain.SandboxPolicyStrict) {
		t.Errorf("expected strict policy, got %s", cfg.GetSandboxPolicy())
	}

	// 3. Nil receiver safety
	var nilConfig *Config
	if nilConfig.GetSandboxPolicy() != string(domain.SandboxPolicyStandard) {
		t.Errorf("expected nil config to return standard policy, got %s", nilConfig.GetSandboxPolicy())
	}
	var nilServer *ServerConfig
	if nilServer.GetSandboxPolicy() != string(domain.SandboxPolicyStandard) {
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

func TestConfig_ReadOnlyGuard(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "custom_config.json")
	content := `{
		"version": 2,
		"server": {
			"model": "custom-model"
		}
	}`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write custom config: %v", err)
	}

	cfg, err := LoadConfigFile(configFile)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}

	if !cfg.ReadOnly {
		t.Fatalf("expected cfg.ReadOnly to be true for externally loaded config")
	}

	// Attempting to save a ReadOnly config must return an error
	if err := cfg.Save(); err == nil {
		t.Errorf("expected cfg.Save() to fail on ReadOnly config, got nil")
	}
}

func TestConfig_IsolatedDirFallback(t *testing.T) {
	// Unset PLEASE_CONFIG_DIR to verify test sandbox fallback
	t.Setenv("PLEASE_CONFIG_DIR", "")
	dir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}
	realConfigDir, _ := os.UserConfigDir()
	if realConfigDir != "" && strings.HasPrefix(dir, realConfigDir) {
		t.Fatalf("CRITICAL: GetConfigDir returned user real config directory %s during test execution!", dir)
	}
}

func TestConfig_SaveBackup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)

	cfg := defaultConfig()
	if err := cfg.Save(); err != nil {
		t.Fatalf("initial Save failed: %v", err)
	}

	// Verify initial config.json exists
	configPath := filepath.Join(tmpDir, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}

	// Mutate and save again
	cfg.Server.Model = "upgraded-model"
	if err := cfg.Save(); err != nil {
		t.Fatalf("second Save failed: %v", err)
	}

	// Verify backup config.json.bak was created
	backupPath := filepath.Join(tmpDir, "config.json.bak")
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("expected config.json.bak to exist: %v", err)
	}

	if strings.Contains(string(backupData), "upgraded-model") {
		t.Errorf("expected backup file to contain previous config without 'upgraded-model'")
	}
}

func TestCanonPresets(t *testing.T) {
	// Locate repository root relative to internal/config
	repoRoot := filepath.Join("..", "..")
	configsDir := filepath.Join(repoRoot, "examples", "configs")

	entries, err := os.ReadDir(configsDir)
	if err != nil {
		t.Skipf("skipping canon presets test; examples/configs not found: %v", err)
		return
	}

	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		found++
		presetPath := filepath.Join(configsDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			cfg, err := LoadConfigFile(presetPath)
			if err != nil {
				t.Fatalf("failed to load canon preset %s: %v", entry.Name(), err)
			}
			if cfg.Version != CurrentConfigVersion {
				t.Errorf("expected version %d, got %d", CurrentConfigVersion, cfg.Version)
			}
			if cfg.Mode == "" {
				t.Errorf("expected mode to be specified")
			}
			if cfg.Mode == "client" {
				if cfg.Client == nil {
					t.Errorf("client mode config requires client block")
				}
			} else {
				if cfg.Server == nil {
					t.Errorf("server/standalone mode config requires server block")
				}
			}
		})
	}

	if found == 0 {
		t.Errorf("expected to find canon presets in %s, found none", configsDir)
	}
}

func TestConfig_EnableBellOnTurnComplete(t *testing.T) {
	// Clean environment for testing
	defer func() {
		os.Unsetenv("NO_BELL")
		os.Unsetenv("PLEASE_BELL")
	}()

	// 1. Default should be true
	cfg := NewDefaultConfig()
	if !cfg.EnableBellOnTurnComplete() {
		t.Errorf("expected EnableBellOnTurnComplete to be true by default")
	}

	// 2. Explicit false
	f := false
	cfg.Client.BellOnTurnComplete = &f
	if cfg.EnableBellOnTurnComplete() {
		t.Errorf("expected EnableBellOnTurnComplete to be false when configured false")
	}

	// 3. Explicit true
	tr := true
	cfg.Client.BellOnTurnComplete = &tr
	if !cfg.EnableBellOnTurnComplete() {
		t.Errorf("expected EnableBellOnTurnComplete to be true when configured true")
	}

	// 4. Nil Client fallback
	var nilCfg *Config
	if !nilCfg.EnableBellOnTurnComplete() {
		t.Errorf("expected nil Config to default to true")
	}

	// 5. Environment variable NO_BELL=1 forces false
	os.Setenv("NO_BELL", "1")
	if cfg.EnableBellOnTurnComplete() {
		t.Errorf("expected NO_BELL=1 to force EnableBellOnTurnComplete false")
	}
	os.Unsetenv("NO_BELL")

	// 6. Environment variable PLEASE_BELL=0 forces false
	os.Setenv("PLEASE_BELL", "0")
	if cfg.EnableBellOnTurnComplete() {
		t.Errorf("expected PLEASE_BELL=0 to force EnableBellOnTurnComplete false")
	}
	os.Unsetenv("PLEASE_BELL")

	// 7. Test flat config migration with "bell": false
	flatJSON := []byte(`{"bell": false, "model": "test-model"}`)
	flatCfg, _, err := migrateConfig(flatJSON)
	if err != nil {
		t.Fatalf("failed to migrate flat config: %v", err)
	}
	if flatCfg.EnableBellOnTurnComplete() {
		t.Errorf("expected migrated flat config with 'bell': false to disable bell")
	}

	// 8. Test flat config migration with "bell": true
	flatJSONTrue := []byte(`{"bell": true, "model": "test-model"}`)
	flatCfgTrue, _, err := migrateConfig(flatJSONTrue)
	if err != nil {
		t.Fatalf("failed to migrate flat config: %v", err)
	}
	if !flatCfgTrue.EnableBellOnTurnComplete() {
		t.Errorf("expected migrated flat config with 'bell': true to enable bell")
	}
}

func TestConfig_OptionsSerialization(t *testing.T) {
	temp := 0.5
	ctxVal := 8192
	cfg := &Config{
		Server: &ServerConfig{
			Provider:    "ollama",
			Model:       "llama3:8b",
			Endpoint:    "http://localhost:11434/api/chat",
			VaultPath:   "vault.db",
			StorageType: "sqlite",
			Options: &domain.ModelOptions{
				Temperature: &temp,
				NumCtx:      &ctxVal,
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if loaded.Server == nil || loaded.Server.Options == nil {
		t.Fatalf("expected server options to be non-nil")
	}
	if loaded.Server.Options.Temperature == nil || *loaded.Server.Options.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %v", loaded.Server.Options.Temperature)
	}
	if loaded.Server.Options.NumCtx == nil || *loaded.Server.Options.NumCtx != 8192 {
		t.Errorf("expected num_ctx 8192, got %v", loaded.Server.Options.NumCtx)
	}
	if loaded.Server.Options.TopP != nil {
		t.Errorf("expected top_p to be nil, got %v", loaded.Server.Options.TopP)
	}
}

func TestConfig_SaveAndLoad_Isolation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)

	temp := 0.6
	cfg := &Config{
		Server: &ServerConfig{
			Provider:    "openai",
			Model:       "gpt-4o-mini",
			Endpoint:    "https://api.openai.com/v1/chat/completions",
			VaultPath:   filepath.Join(tmpDir, "vault.db"),
			StorageType: "sqlite",
			Options: &domain.ModelOptions{
				Temperature: &temp,
			},
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config to isolated dir: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config from isolated dir: %v", err)
	}

	if loaded.Server == nil || loaded.Server.Model != "gpt-4o-mini" {
		t.Errorf("expected model gpt-4o-mini, got %v", loaded.Server)
	}
	if loaded.Server.Options == nil || loaded.Server.Options.Temperature == nil || *loaded.Server.Options.Temperature != 0.6 {
		t.Errorf("expected temperature 0.6, got %v", loaded.Server.Options)
	}
}

func TestConfig_WorkspaceDir(t *testing.T) {
	// 1. Unset returns "."
	var emptyCfg Config
	if emptyCfg.GetWorkspaceDir() != "." {
		t.Errorf("expected empty WorkspaceDir to return '.', got %s", emptyCfg.GetWorkspaceDir())
	}

	// 2. Custom path returns absolute path
	tmpDir := t.TempDir()
	cfg := Config{Server: &ServerConfig{WorkspaceDir: tmpDir}}
	absTmp, _ := filepath.Abs(tmpDir)
	if cfg.GetWorkspaceDir() != absTmp {
		t.Errorf("expected %s, got %s", absTmp, cfg.GetWorkspaceDir())
	}

	// 3. Tilde expansion & trailing slash handling
	home, _ := os.UserHomeDir()
	tildeCfg := Config{Server: &ServerConfig{WorkspaceDir: "~/my-project"}}
	expectedTilde := filepath.Join(home, "my-project")
	if tildeCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s, got %s", expectedTilde, tildeCfg.GetWorkspaceDir())
	}

	tildeSlashCfg := Config{Server: &ServerConfig{WorkspaceDir: "~/my-project/"}}
	if tildeSlashCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s for trailing slash, got %s", expectedTilde, tildeSlashCfg.GetWorkspaceDir())
	}

	// 4. Environment variable expansion ($HOME)
	envCfg := Config{Server: &ServerConfig{WorkspaceDir: "$HOME/my-project"}}
	if envCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s for $HOME expansion, got %s", expectedTilde, envCfg.GetWorkspaceDir())
	}

	envBraceCfg := Config{Server: &ServerConfig{WorkspaceDir: "${HOME}/my-project/"}}
	if envBraceCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s for ${HOME} with trailing slash, got %s", expectedTilde, envBraceCfg.GetWorkspaceDir())
	}

	// 5. JSON roundtrip
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	if loaded.Server == nil || loaded.Server.WorkspaceDir != tmpDir {
		t.Errorf("expected WorkspaceDir %s, got %v", tmpDir, loaded.Server)
	}
}

func TestConfig_VaultPath_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("skipping test; home dir not available")
	}

	cfg := Config{
		Server: &ServerConfig{
			VaultPath: "~/.please/vault.db",
		},
	}

	expected := filepath.Join(home, ".please", "vault.db")
	if cfg.GetVaultPath() != expected {
		t.Errorf("expected %s, got %s", expected, cfg.GetVaultPath())
	}

	// Environment variable expansion
	envCfg := Config{
		Server: &ServerConfig{
			VaultPath: "$HOME/.please/vault.db",
		},
	}
	if envCfg.GetVaultPath() != expected {
		t.Errorf("expected %s, got %s", expected, envCfg.GetVaultPath())
	}
}

func TestConfig_GlobalPleaseDir_Isolation(t *testing.T) {
	isolatedDir := t.TempDir()
	t.Setenv("PLEASE_GLOBAL_DIR", isolatedDir)

	globalDir, err := GetGlobalPleaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if globalDir != isolatedDir {
		t.Errorf("expected global dir %s, got %s", isolatedDir, globalDir)
	}
}

func TestConfig_FindWorkspaceRoot_And_PleaseDir(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		tmpDir = t.TempDir()
	}

	// 1. Plain directory (no .git, no .please)
	root, found := FindWorkspaceRoot(tmpDir)
	if found {
		t.Errorf("expected found=false for plain directory, got root=%s", root)
	}

	// 2. Directory with .git boundary
	gitDir := filepath.Join(tmpDir, "my-repo")
	_ = os.MkdirAll(filepath.Join(gitDir, ".git"), 0755)
	subPkg := filepath.Join(gitDir, "pkg", "subpkg")
	_ = os.MkdirAll(subPkg, 0755)

	// In gitDir itself: has .git boundary
	root, found = FindWorkspaceRoot(gitDir)
	if !found || root != gitDir {
		t.Errorf("expected found=true with root=%s, got found=%v root=%s", gitDir, found, root)
	}

	// In subPkg: zero crawling upward! It should NOT climb to gitDir
	rootSub, foundSub := FindWorkspaceRoot(subPkg)
	if foundSub {
		t.Errorf("expected foundSub=false (zero crawling), got rootSub=%s", rootSub)
	}

	// Without .please in subPkg, GetWorkspacePleaseDir returns false
	pleaseDir, hasPlease := GetWorkspacePleaseDir(subPkg)
	if hasPlease {
		t.Errorf("expected hasPlease=false before .please is initialized, got %s", pleaseDir)
	}

	// 3. Directory with .please initialized in gitDir
	expectedPlease := filepath.Join(gitDir, ".please")
	_ = os.MkdirAll(expectedPlease, 0755)

	// GetWorkspacePleaseDir in gitDir finds it directly
	pleaseDir, hasPlease = GetWorkspacePleaseDir(gitDir)
	if !hasPlease || pleaseDir != expectedPlease {
		t.Errorf("expected hasPlease=true with pleaseDir=%s, got hasPlease=%v pleaseDir=%s", expectedPlease, hasPlease, pleaseDir)
	}

	// But in subPkg: ZERO climbing! It should NOT find gitDir/.please
	pleaseSubDir, hasSubPlease := GetWorkspacePleaseDir(subPkg)
	if hasSubPlease {
		t.Errorf("expected hasSubPlease=false (no upward crawling), got %s", pleaseSubDir)
	}

	// 4. Verification: globalDir (~/.please) is never mistaken for a workspace dot-directory
	globalFakeDir := filepath.Join(tmpDir, "fake-home", ".please")
	_ = os.MkdirAll(globalFakeDir, 0755)
	t.Setenv("PLEASE_GLOBAL_DIR", globalFakeDir)

	wsDir, isWs := GetWorkspacePleaseDir(filepath.Join(tmpDir, "fake-home"))
	if isWs {
		t.Errorf("expected globalDir not to be identified as workspace, got %s", wsDir)
	}
}

func TestConfig_DiscoveryLadder_WorkspaceCascade(t *testing.T) {
	t.Setenv("PLEASE_CONFIG_DIR", "")
	globalDir := t.TempDir()
	t.Setenv("PLEASE_GLOBAL_DIR", globalDir)

	// Write global config with baseline settings
	globalCfg := defaultConfig()
	globalCfg.Server.Model = "gemma4:base-global"
	globalCfg.Server.Endpoint = "http://localhost:11434/api/chat"
	if err := globalCfg.Save(); err != nil {
		t.Fatalf("failed to save global config: %v", err)
	}

	// Create workspace with .please/
	wsDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		wsDir = t.TempDir()
	}
	wsPlease := filepath.Join(wsDir, ".please")
	_ = os.MkdirAll(wsPlease, 0755)

	// Write workspace config overriding model
	wsConfigData := `{
		"version": 2,
		"server": {
			"model": "gemma4:project-override",
			"signat_steering": true
		}
	}`
	if err := os.WriteFile(filepath.Join(wsPlease, "config.json"), []byte(wsConfigData), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	// Temporarily chdir to workspace to test upward discovery ladder
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(wsDir)

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Assertions:
	// 1. Model overridden from workspace config
	if loaded.Server.Model != "gemma4:project-override" {
		t.Errorf("expected overridden model 'gemma4:project-override', got %q", loaded.Server.Model)
	}
	// 2. Endpoint inherited from global config
	if loaded.Server.Endpoint != "http://localhost:11434/api/chat" {
		t.Errorf("expected inherited endpoint, got %q", loaded.Server.Endpoint)
	}
	// 3. Vault automatically bound to .please/vault.db
	expectedVault := filepath.Join(wsPlease, "vault.db")
	if loaded.Server.VaultPath != expectedVault {
		t.Errorf("expected vault path %s, got %s", expectedVault, loaded.Server.VaultPath)
	}
	// 4. Origin tracking and badges
	if !loaded.IsWorkspaceActive() {
		t.Errorf("expected workspace to be active")
	}
	if loaded.GetOrigin("server.model") != OriginWorkspace {
		t.Errorf("expected origin %q for server.model, got %q", OriginWorkspace, loaded.GetOrigin("server.model"))
	}
	if loaded.OriginBadge("server.model") != "[workspace override]" {
		t.Errorf("expected badge '[workspace override]', got %q", loaded.OriginBadge("server.model"))
	}
	if loaded.GetOrigin("server.endpoint") != OriginGlobal {
		t.Errorf("expected origin %q for server.endpoint, got %q", OriginGlobal, loaded.GetOrigin("server.endpoint"))
	}
	if loaded.OriginBadge("server.endpoint") != "[global preference]" {
		t.Errorf("expected badge '[global preference]', got %q", loaded.OriginBadge("server.endpoint"))
	}
	if loaded.GetOrigin("server.vault_path") != OriginAnchor {
		t.Errorf("expected origin %q for server.vault_path, got %q", OriginAnchor, loaded.GetOrigin("server.vault_path"))
	}
	if loaded.OriginBadge("server.vault_path") != "[workspace anchor]" {
		t.Errorf("expected badge '[workspace anchor]', got %q", loaded.OriginBadge("server.vault_path"))
	}
}

func TestConfig_SaveScoped_OptionA(t *testing.T) {
	t.Setenv("PLEASE_CONFIG_DIR", "")
	globalDir := t.TempDir()
	t.Setenv("PLEASE_GLOBAL_DIR", globalDir)

	// Write baseline global config
	globalCfg := defaultConfig()
	globalCfg.Server.Model = "baseline-global-model"
	if err := globalCfg.Save(); err != nil {
		t.Fatalf("failed to save initial global config: %v", err)
	}

	// Create workspace
	wsDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		wsDir = t.TempDir()
	}
	wsPlease := filepath.Join(wsDir, ".please")
	_ = os.MkdirAll(wsPlease, 0755)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(wsDir)

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// 1. Save a Project Setting (e.g. Model) -> must persist to workspace .please/config.json
	loaded.Server.Model = "custom-project-model"
	savedWsPath, err := loaded.SaveScoped(true)
	if err != nil {
		t.Fatalf("SaveScoped(true) failed: %v", err)
	}
	expectedWsConfig := filepath.Join(wsPlease, "config.json")
	if savedWsPath != expectedWsConfig {
		t.Errorf("expected saved path %s, got %s", expectedWsConfig, savedWsPath)
	}
	wsBytes, err := os.ReadFile(expectedWsConfig)
	if err != nil {
		t.Fatalf("failed to read workspace config: %v", err)
	}
	if !strings.Contains(string(wsBytes), "custom-project-model") {
		t.Errorf("workspace config missing project setting: %s", string(wsBytes))
	}
	// Verify workspace config does NOT leak global client settings or global encryption key
	if strings.Contains(string(wsBytes), `"natural_pacing"`) {
		t.Errorf("workspace config should not contain global client settings: %s", string(wsBytes))
	}

	// 2. Save a Client Setting (e.g. Natural Pacing) -> must persist to global ~/.please/config.json
	pacingVal := false
	loaded.Client.NaturalPacing = &pacingVal
	savedGlobalPath, err := loaded.SaveScoped(false)
	if err != nil {
		t.Fatalf("SaveScoped(false) failed: %v", err)
	}
	expectedGlobalConfig := filepath.Join(globalDir, "config.json")
	if savedGlobalPath != expectedGlobalConfig {
		t.Errorf("expected global path %s, got %s", expectedGlobalConfig, savedGlobalPath)
	}
	globalBytes, err := os.ReadFile(expectedGlobalConfig)
	if err != nil {
		t.Fatalf("failed to read global config: %v", err)
	}
	if !strings.Contains(string(globalBytes), `"natural_pacing": false`) {
		t.Errorf("global config missing client setting: %s", string(globalBytes))
	}
	// CRITICAL PROTECTION: Global config must NOT have been polluted by workspace's model override!
	if strings.Contains(string(globalBytes), "custom-project-model") {
		t.Errorf("FATAL: global config was polluted with workspace model override: %s", string(globalBytes))
	}
	if !strings.Contains(string(globalBytes), "baseline-global-model") {
		t.Errorf("global config lost its original model setting: %s", string(globalBytes))
	}
}

func TestConfig_EncryptionKey_CascadeAndEnv(t *testing.T) {
	t.Setenv("PLEASE_CONFIG_DIR", "")
	globalDir := t.TempDir()
	t.Setenv("PLEASE_GLOBAL_DIR", globalDir)

	// Baseline global config with an encryption key
	globalCfg := defaultConfig()
	globalCfg.Server.EncryptionKey = "global-secret-key"
	if err := globalCfg.Save(); err != nil {
		t.Fatalf("failed to save global config: %v", err)
	}

	wsDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		wsDir = t.TempDir()
	}
	wsPlease := filepath.Join(wsDir, ".please")
	_ = os.MkdirAll(wsPlease, 0755)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(wsDir)

	// 1. Inherit global key by default in workspace
	cfg1, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg1.Server.EncryptionKey != "global-secret-key" {
		t.Errorf("expected global encryption key, got %q", cfg1.Server.EncryptionKey)
	}
	if cfg1.OriginBadge("server.encryption_key") != "[global preference]" {
		t.Errorf("expected [global preference], got %q", cfg1.OriginBadge("server.encryption_key"))
	}

	// 2. Workspace override
	wsConfigData := `{"version": 2, "server": {"encryption_key": "workspace-isolated-key"}}`
	_ = os.WriteFile(filepath.Join(wsPlease, "config.json"), []byte(wsConfigData), 0644)
	cfg2, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg2.Server.EncryptionKey != "workspace-isolated-key" {
		t.Errorf("expected workspace encryption key, got %q", cfg2.Server.EncryptionKey)
	}
	if cfg2.OriginBadge("server.encryption_key") != "[workspace override]" {
		t.Errorf("expected [workspace override], got %q", cfg2.OriginBadge("server.encryption_key"))
	}

	// 3. Workspace opt-out ("none")
	wsOptOutData := `{"version": 2, "server": {"encryption_key": "none"}}`
	_ = os.WriteFile(filepath.Join(wsPlease, "config.json"), []byte(wsOptOutData), 0644)
	cfg3, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg3.Server.EncryptionKey != "" {
		t.Errorf("expected empty encryption key after opt-out, got %q", cfg3.Server.EncryptionKey)
	}
	if cfg3.OriginBadge("server.encryption_key") != "[workspace opt-out]" {
		t.Errorf("expected [workspace opt-out], got %q", cfg3.OriginBadge("server.encryption_key"))
	}

	// 4. Environment variable precedence
	t.Setenv("PLEASE_ENCRYPTION_KEY", "env-injected-secret")
	cfg4, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg4.Server.EncryptionKey != "env-injected-secret" {
		t.Errorf("expected env encryption key, got %q", cfg4.Server.EncryptionKey)
	}
	if cfg4.OriginBadge("server.encryption_key") != "[env override]" {
		t.Errorf("expected [env override], got %q", cfg4.OriginBadge("server.encryption_key"))
	}
}
