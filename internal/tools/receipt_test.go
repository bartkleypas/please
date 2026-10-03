package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/storage"
)

type mockObservationStore struct {
	blobs map[string]*storage.ObservationBlob
}

func (m *mockObservationStore) GetObservationBlob(receiptID string) (*storage.ObservationBlob, error) {
	if blob, ok := m.blobs[receiptID]; ok {
		return blob, nil
	}
	return nil, fmt.Errorf("not found: %s", receiptID)
}

func TestInspectReceiptTool_Paging(t *testing.T) {
	store := &mockObservationStore{
		blobs: make(map[string]*storage.ObservationBlob),
	}

	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString(fmt.Sprintf("output line %d\n", i))
	}

	receiptID := "obs_12345678"
	store.blobs[receiptID] = &storage.ObservationBlob{
		ReceiptID: receiptID,
		NodeID:    "node-1",
		CreatedAt: time.Now(),
		Tool:      "execute_command",
		Payload:   []byte(strings.TrimRight(sb.String(), "\n")),
	}

	tool := InspectReceiptTool(store, nil)

	// Page 1: default lines 1-50
	res, err := tool.Function(context.Background(), map[string]interface{}{
		"receipt_id": receiptID,
	})
	if err != nil {
		t.Fatalf("tool failed: %v", err)
	}

	if !strings.Contains(res, "Showing lines 1-50 of 100") {
		t.Errorf("expected header with lines 1-50, got:\n%s", res)
	}
	if !strings.Contains(res, "   1: output line 1") {
		t.Errorf("expected line 1 formatted, got:\n%s", res)
	}
	if !strings.Contains(res, "  50: output line 50") {
		t.Errorf("expected line 50 formatted, got:\n%s", res)
	}
	if !strings.Contains(res, "50 more lines omitted. Use start_line=51 to continue.") {
		t.Errorf("expected continuation notice, got:\n%s", res)
	}

	// Page 2: lines 51-100
	res2, err := tool.Function(context.Background(), map[string]interface{}{
		"receipt_id": receiptID,
		"start_line": float64(51),
		"line_count": float64(50),
	})
	if err != nil {
		t.Fatalf("page 2 failed: %v", err)
	}
	if !strings.Contains(res2, "Showing lines 51-100 of 100") {
		t.Errorf("expected header with lines 51-100, got:\n%s", res2)
	}
	if strings.Contains(res2, "more lines omitted") {
		t.Errorf("expected no lines omitted on last page, got:\n%s", res2)
	}
}

func TestInspectReceiptTool_Grep(t *testing.T) {
	store := &mockObservationStore{
		blobs: make(map[string]*storage.ObservationBlob),
	}

	payload := `=== RUN   TestSessionHarness
=== RUN   TestSessionHarness/Step1
--- PASS: TestSessionHarness/Step1 (0.01s)
=== RUN   TestSessionHarness/Step2
--- FAIL: TestSessionHarness/Step2 (0.02s)
    harness_test.go:42: assertion failure: expected 42 got 0
FAIL
FAIL	github.com/bartkleypas/please/internal/engine`

	receiptID := "obs_fail1234"
	store.blobs[receiptID] = &storage.ObservationBlob{
		ReceiptID: receiptID,
		Payload:   []byte(payload),
	}

	tool := InspectReceiptTool(store, nil)

	// Grep for FAIL:
	res, err := tool.Function(context.Background(), map[string]interface{}{
		"receipt_id": receiptID,
		"grep":       "FAIL:",
	})
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	if !strings.Contains(res, "5: --- FAIL: TestSessionHarness/Step2") {
		t.Errorf("expected line 5 with original line number, got:\n%s", res)
	}
	if strings.Contains(res, "Step1") {
		t.Errorf("unexpected matching line for Step1 in grep output")
	}
}

func TestInspectReceiptTool_LegacyFallback(t *testing.T) {
	legacyData := "legacy observation text from existing node"
	tool := InspectReceiptTool(nil, func(receiptID string) (string, error) {
		if receiptID == "obs_legacy99" {
			return legacyData, nil
		}
		return "", fmt.Errorf("not found")
	})

	res, err := tool.Function(context.Background(), map[string]interface{}{
		"receipt_id": "obs_legacy99",
	})
	if err != nil {
		t.Fatalf("fallback failed: %v", err)
	}

	if !strings.Contains(res, "legacy observation text") {
		t.Errorf("expected legacy text in response, got:\n%s", res)
	}
}
