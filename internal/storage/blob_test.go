package storage

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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
