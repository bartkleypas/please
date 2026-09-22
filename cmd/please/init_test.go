package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/storage"
)

func TestInit_Workspace(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		tmpDir = t.TempDir()
	}

	// Fake a git repository root
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("failed to create fake .git: %v", err)
	}

	// 1. First initialization
	if err := executeWorkspaceInit(tmpDir, false); err != nil {
		t.Fatalf("executeWorkspaceInit failed: %v", err)
	}

	pleaseDir := filepath.Join(tmpDir, ".please")
	if fi, err := os.Stat(pleaseDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected .please directory to exist at %s", pleaseDir)
	}

	configPath := filepath.Join(pleaseDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("expected .please/config.json to exist")
	}

	vaultPath := filepath.Join(pleaseDir, "vault.db")
	if _, err := os.Stat(vaultPath); os.IsNotExist(err) {
		t.Fatalf("expected .please/vault.db to exist")
	}

	// Verify database is valid SQLite with Genesis node
	store, err := storage.NewSQLiteStorage(vaultPath, "")
	if err != nil {
		t.Fatalf("failed to open initialized vault: %v", err)
	}
	g, _, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("failed to load graph from initialized vault: %v", err)
	}
	if len(g.Nodes) == 0 {
		t.Errorf("expected initialized vault to contain Genesis root node")
	}

	// Verify .gitignore was updated
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), ".please/") {
		t.Errorf("expected .gitignore to contain '.please/', got:\n%s", string(data))
	}

	// 2. Re-initialization (idempotence test)
	if err := executeWorkspaceInit(tmpDir, false); err != nil {
		t.Fatalf("re-initialization failed: %v", err)
	}

	// Verify .gitignore does not have duplicate entries
	data2, _ := os.ReadFile(gitignorePath)
	count := strings.Count(string(data2), ".please/")
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of '.please/' in .gitignore, found %d", count)
	}
}

func TestInit_Global_Isolated(t *testing.T) {
	// STRICT ISOLATION GUARD:
	// Verify that global initialization uses an isolated temporary directory
	// and never touches the developer's real home directory.
	fakeHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		fakeHome = t.TempDir()
	}

	isolatedGlobalDir := filepath.Join(fakeHome, ".please")
	t.Setenv("HOME", fakeHome)
	t.Setenv("PLEASE_GLOBAL_DIR", isolatedGlobalDir)

	if err := executeGlobalInit(false); err != nil {
		t.Fatalf("executeGlobalInit failed: %v", err)
	}

	if fi, err := os.Stat(isolatedGlobalDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected isolated global dir to exist: %v", err)
	}

	configPath := filepath.Join(isolatedGlobalDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("expected global config.json to exist")
	}

	vaultPath := filepath.Join(isolatedGlobalDir, "vault.db")
	if _, err := os.Stat(vaultPath); os.IsNotExist(err) {
		t.Errorf("expected global vault.db to exist")
	}

	// Confirm the vault is readable and contains a Genesis node
	store, err := storage.NewSQLiteStorage(vaultPath, "")
	if err != nil {
		t.Fatalf("failed to open global vault: %v", err)
	}
	g, _, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}
	if len(g.Nodes) == 0 {
		t.Errorf("expected global vault to have Genesis node")
	}
}
