package storage

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
)

func TestObservationBlob_SaveAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")

	store, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	payloadText := strings.Repeat("Line of build telemetry and compiler output\n", 100)
	blob := &ObservationBlob{
		ReceiptID: "obs_test1234",
		NodeID:    "node-abc",
		CreatedAt: time.Now(),
		Tool:      "execute_command",
		Payload:   []byte(payloadText),
	}

	if err := store.SaveObservationBlob(blob); err != nil {
		t.Fatalf("SaveObservationBlob failed: %v", err)
	}

	// Verify compressed payload in database
	var compressed bool
	var rawBytes []byte
	err = store.db.QueryRow("SELECT compressed, payload FROM observation_blobs WHERE receipt_id = ?", blob.ReceiptID).Scan(&compressed, &rawBytes)
	if err != nil {
		t.Fatalf("failed to query raw row: %v", err)
	}
	if !compressed {
		t.Errorf("expected blob to be stored compressed")
	}
	if len(rawBytes) >= len(payloadText) {
		t.Errorf("expected compression to reduce size, raw=%d vs stored=%d", len(payloadText), len(rawBytes))
	}

	// Retrieve blob and verify decompressed payload
	retrieved, err := store.GetObservationBlob(blob.ReceiptID)
	if err != nil {
		t.Fatalf("GetObservationBlob failed: %v", err)
	}

	if retrieved.ReceiptID != blob.ReceiptID {
		t.Errorf("receipt ID mismatch: %s vs %s", retrieved.ReceiptID, blob.ReceiptID)
	}
	if retrieved.Tool != blob.Tool {
		t.Errorf("tool mismatch: %s vs %s", retrieved.Tool, blob.Tool)
	}
	if string(retrieved.Payload) != payloadText {
		t.Errorf("payload mismatch after decompression")
	}
}

func TestObservationBlob_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")

	store, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	_, err = store.GetObservationBlob("obs_nonexistent")
	if err == nil {
		t.Fatalf("expected error for nonexistent receipt, got nil")
	}
}

func TestObservationBlob_EncryptedSaveAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault_enc.db")
	key := "blob-secret-key-99"

	store, err := NewSQLiteStorage(dbPath, key)
	if err != nil {
		t.Fatalf("failed to create encrypted sqlite storage: %v", err)
	}

	payloadText := strings.Repeat("Sensitive compiler trace line\n", 80)
	blob := &ObservationBlob{
		ReceiptID: "obs_enc12345",
		NodeID:    "node-enc",
		CreatedAt: time.Now(),
		Tool:      "execute_command",
		Payload:   []byte(payloadText),
	}

	if err := store.SaveObservationBlob(blob); err != nil {
		t.Fatalf("SaveObservationBlob failed: %v", err)
	}

	var rawBytes []byte
	if err := store.db.QueryRow("SELECT payload FROM observation_blobs WHERE receipt_id = ?", blob.ReceiptID).Scan(&rawBytes); err != nil {
		t.Fatalf("failed to query raw blob payload: %v", err)
	}
	if !strings.HasPrefix(string(rawBytes), encryptionPrefix) {
		t.Fatalf("expected raw blob payload to start with %q, got: %q", encryptionPrefix, string(rawBytes))
	}

	retrieved, err := store.GetObservationBlob(blob.ReceiptID)
	if err != nil {
		t.Fatalf("GetObservationBlob failed: %v", err)
	}
	if string(retrieved.Payload) != payloadText {
		t.Errorf("decrypted blob payload mismatch")
	}

	// Wrong key must fail to decrypt
	wrongStore, err := NewSQLiteStorage(dbPath, "wrong-key")
	if err != nil {
		t.Fatalf("failed to open wrong-key storage: %v", err)
	}
	if _, err := wrongStore.GetObservationBlob(blob.ReceiptID); err == nil {
		t.Errorf("expected decryption error with wrong key, got nil")
	}
}

func TestObservationBlob_GarbageCollectPurgesOrphans(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault_gc.db")

	store, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	keepNode := &graph.Node{
		ID:        "node-keep",
		Role:      domain.RoleAssistant,
		Content:   "kept",
		Timestamp: time.Now(),
	}
	delNode := &graph.Node{
		ID:        "node-delete",
		Role:      domain.RoleAssistant,
		Content:   "deleted",
		Timestamp: time.Now(),
		Deleted:   true,
	}
	_ = store.SaveNode(keepNode)
	_ = store.SaveNode(delNode)

	_ = store.SaveObservationBlob(&ObservationBlob{
		ReceiptID: "obs_keep1111",
		NodeID:    "node-keep",
		Tool:      "read_file",
		Payload:   []byte("keep payload"),
	})
	_ = store.SaveObservationBlob(&ObservationBlob{
		ReceiptID: "obs_del22222",
		NodeID:    "node-delete",
		Tool:      "execute_command",
		Payload:   []byte("delete payload"),
	})

	purged, err := store.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect failed: %v", err)
	}
	if purged != 1 {
		t.Errorf("expected 1 deleted node, got %d", purged)
	}

	if _, err := store.GetObservationBlob("obs_keep1111"); err != nil {
		t.Errorf("expected kept blob to survive GC, got: %v", err)
	}
	if _, err := store.GetObservationBlob("obs_del22222"); err == nil {
		t.Errorf("expected orphaned blob obs_del22222 to be purged by GC")
	}
}
