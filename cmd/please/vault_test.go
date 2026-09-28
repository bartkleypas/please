package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/storage"
)

func TestDecryptSQLiteVault(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "vault_encrypted.db")
	key := "test-secret-key-12345"

	// 1. Create an encrypted SQLite vault
	store, err := storage.NewSQLiteStorage(dbPath, key)
	if err != nil {
		t.Fatalf("failed to create encrypted storage: %v", err)
	}

	node := &graph.Node{
		ID:        "node-001",
		Role:      domain.RoleAssistant,
		Content:   "Sensitive encrypted content",
		Thought:   "Sensitive thought process",
		Timestamp: time.Now(),
	}
	if err := store.SaveNode(node); err != nil {
		t.Fatalf("failed to save encrypted node: %v", err)
	}

	mem := &storage.Memory{
		ID:         "mem-001",
		Key:        "arch:test:secret",
		Content:    "Encrypted memory rule",
		Category:   storage.CategoryArchitecture,
		Scope:      storage.ScopeWorkspace,
		Confidence: 1.0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("failed to save encrypted memory: %v", err)
	}

	// Verify at least one row in SQLite starts with enc:v1:
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw sqlite: %v", err)
	}
	var rawContent string
	if err := db.QueryRow("SELECT content FROM nodes WHERE id = 'node-001';").Scan(&rawContent); err != nil {
		t.Fatalf("failed to query raw node: %v", err)
	}
	db.Close()

	if len(rawContent) < 7 || rawContent[:7] != "enc:v1:" {
		t.Fatalf("expected raw content to be encrypted (enc:v1:), got: %s", rawContent)
	}

	// 2. Decrypt using decryptSQLiteVault
	nodesDec, memsDec, err := decryptSQLiteVault(dbPath, key)
	if err != nil {
		t.Fatalf("failed to decrypt vault: %v", err)
	}
	if nodesDec != 1 {
		t.Errorf("expected 1 node decrypted, got %d", nodesDec)
	}
	if memsDec != 1 {
		t.Errorf("expected 1 memory decrypted, got %d", memsDec)
	}

	// 3. Verify content is now raw plaintext in raw SQLite
	db2, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to reopen sqlite: %v", err)
	}
	defer db2.Close()

	var plainNodeContent, plainThought string
	if err := db2.QueryRow("SELECT content, thought FROM nodes WHERE id = 'node-001';").Scan(&plainNodeContent, &plainThought); err != nil {
		t.Fatalf("failed to query decrypted node: %v", err)
	}
	if plainNodeContent != "Sensitive encrypted content" {
		t.Errorf("expected plaintext node content, got: %s", plainNodeContent)
	}
	if plainThought != "Sensitive thought process" {
		t.Errorf("expected plaintext thought, got: %s", plainThought)
	}

	var plainMemContent string
	if err := db2.QueryRow("SELECT content FROM memories WHERE key = 'arch:test:secret';").Scan(&plainMemContent); err != nil {
		t.Fatalf("failed to query decrypted memory: %v", err)
	}
	if plainMemContent != "Encrypted memory rule" {
		t.Errorf("expected plaintext memory content, got: %s", plainMemContent)
	}
}
