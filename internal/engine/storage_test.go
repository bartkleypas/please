package engine

import (
	"path/filepath"
	"testing"
	"time"
)

func TestEngineStorage_ReExportsAndAliases(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Verify NewSQLiteStorage via engine re-export
	sqlitePath := filepath.Join(tmpDir, "test_vault.db")
	sqliteStorage, err := NewSQLiteStorage(sqlitePath, "test-key-123")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	// Verify Storage interface assignment
	var _ Storage = sqliteStorage

	now := time.Now()
	node := &Node{
		ID:        "node-engine-1",
		Role:      RoleUser,
		Content:   "Testing engine storage re-export",
		Timestamp: now,
	}

	if err := sqliteStorage.SaveNode(node); err != nil {
		t.Fatalf("SaveNode failed via engine re-export: %v", err)
	}

	g, lastID, err := sqliteStorage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}
	if len(g.Nodes) != 1 || lastID != "node-engine-1" {
		t.Errorf("expected 1 node with lastID node-engine-1, got %d and %s", len(g.Nodes), lastID)
	}

	// 2. Verify NewJSONLStorage via engine re-export
	jsonlPath := filepath.Join(tmpDir, "test_vault.jsonl")
	jsonlStorage := NewJSONLStorage(jsonlPath, "")
	var _ Storage = jsonlStorage

	if err := jsonlStorage.SaveNode(node); err != nil {
		t.Fatalf("SaveNode on JSONL failed: %v", err)
	}

	// 3. Verify NewRemoteDaemonStorage constructor re-export
	remoteStorage, err := NewRemoteDaemonStorage("http://127.0.0.1:8080", "token", "")
	if err != nil {
		t.Fatalf("NewRemoteDaemonStorage failed: %v", err)
	}
	if remoteStorage.SessionID == "" {
		t.Errorf("expected non-empty SessionID on RemoteDaemonStorage")
	}

	// 4. Verify EncryptField and DecryptField re-exports
	cipherText, err := EncryptField("secret data", "key")
	if err != nil {
		t.Fatalf("EncryptField failed: %v", err)
	}
	plainText, err := DecryptField(cipherText, "key")
	if err != nil {
		t.Fatalf("DecryptField failed: %v", err)
	}
	if plainText != "secret data" {
		t.Errorf("expected 'secret data', got '%s'", plainText)
	}
}
