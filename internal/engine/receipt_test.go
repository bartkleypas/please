package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
)

func TestSmartReceipt_CreationAndFormatting(t *testing.T) {
	output := `=== RUN   TestPaging
=== RUN   TestPaging/FailStep
--- FAIL: TestPaging/FailStep (0.01s)
    paging_test.go:12: failure details
FAIL`

	receipt := CreateSmartReceipt("execute_command", map[string]interface{}{
		"command": "go test ./...",
	}, output, true)

	if !strings.HasPrefix(receipt.ReceiptID, "obs_") {
		t.Errorf("expected receipt ID starting with obs_, got %s", receipt.ReceiptID)
	}
	if receipt.Lines != 5 {
		t.Errorf("expected 5 lines, got %d", receipt.Lines)
	}
	if !strings.Contains(receipt.Banner, "FAIL: TestPaging/FailStep") {
		t.Errorf("expected banner with failure, got %s", receipt.Banner)
	}
	if !receipt.HasBlob {
		t.Errorf("expected HasBlob true")
	}

	formatted := FormatReceiptString(receipt)
	if !strings.Contains(formatted, receipt.ReceiptID) {
		t.Errorf("expected receipt string to contain ID, got: %s", formatted)
	}
	if !strings.Contains(formatted, "inspect_receipt") {
		t.Errorf("expected dereference instruction in receipt string")
	}
}

func TestHarness_ObservationBlob_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")

	store, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to init storage: %v", err)
	}

	g := graph.NewGraph()
	mgr := NewManager(g, store)
	mgr.RegisterDefaultTools(tmpDir)

	logPath := filepath.Join(tmpDir, "telemetry.log")
	_ = os.WriteFile(logPath, []byte(strings.Repeat("telemetry log entry line with data\n", 80)), 0644)

	callCount := 0
	p := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, tools []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			callCount++
			if callCount == 1 {
				return "", "Scanning log...", []domain.ToolCall{
					{
						ID:   "call_cmd1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "read_file",
							Arguments: json.RawMessage(`{"path": "telemetry.log"}`),
						},
					},
				}, nil
			}
			return "I analyzed the receipt.", "Done", nil, nil
		},
	}

	cfg := &config.Config{
		Server: &config.ServerConfig{
			SandboxPolicy: "permissive",
		},
	}
	harness := NewSessionHarness(mgr, p, cfg)

	// Execute turn
	asstNode, err := harness.ExecuteTurn(context.Background(), TurnRequest{
		SessionID: "sess-roundtrip",
		Message:   "Check telemetry log",
	}, nil)
	if err != nil {
		t.Fatalf("turn failed: %v", err)
	}

	if asstNode == nil {
		t.Fatalf("expected assistant node returned")
	}

	// Verify observation blob was saved to SQLite and structured receipt attached
	expectedReceiptID := GenerateReceiptID("read_file", []byte(asstNode.Observations[0].Result))
	blob, err := store.GetObservationBlob(expectedReceiptID)
	if err != nil {
		t.Fatalf("failed to retrieve observation blob: %v", err)
	}

	if blob.ReceiptID != expectedReceiptID {
		t.Errorf("expected receipt ID %s, got %s", expectedReceiptID, blob.ReceiptID)
	}

	if asstNode.Observations[0].Receipt == nil {
		t.Fatalf("expected structured SmartReceipt on observation, got nil")
	}
	if asstNode.Observations[0].Receipt.ReceiptID != expectedReceiptID {
		t.Errorf("expected receipt ID %s, got %s", expectedReceiptID, asstNode.Observations[0].Receipt.ReceiptID)
	}
	if asstNode.Observations[0].BlobID != expectedReceiptID {
		t.Errorf("expected blob ID %s, got %s", expectedReceiptID, asstNode.Observations[0].BlobID)
	}

	// Verify persistence in SQLite retains structured receipt
	loadedGraph, _, err := store.LoadGraph()
	if err != nil || loadedGraph == nil {
		t.Fatalf("failed to load graph from storage: %v", err)
	}
	loadedNode, err := loadedGraph.GetNode(asstNode.ID)
	if err != nil || loadedNode == nil {
		t.Fatalf("failed to get node from loaded graph: %v", err)
	}
	if len(loadedNode.Observations) == 0 || loadedNode.Observations[0].Receipt == nil {
		t.Fatalf("expected loaded node to have structured receipt: %+v", loadedNode.Observations)
	}
	if loadedNode.Observations[0].Receipt.ReceiptID != expectedReceiptID {
		t.Errorf("persisted receipt ID mismatch: expected %s, got %s", expectedReceiptID, loadedNode.Observations[0].Receipt.ReceiptID)
	}

	// Now execute inspect_receipt using manager's registry
	inspectTool, ok := mgr.Registry.Tools["inspect_receipt"]
	if !ok {
		t.Fatalf("inspect_receipt not found in registry")
	}

	inspectRes, err := inspectTool.Function(context.Background(), map[string]interface{}{
		"receipt_id": expectedReceiptID,
	})
	if err != nil {
		t.Fatalf("inspect_receipt failed: %v", err)
	}

	if !strings.Contains(inspectRes, expectedReceiptID) {
		t.Errorf("expected inspect output to contain receipt ID, got:\n%s", inspectRes)
	}

	// Step forward to Turn 2 and build LLM context
	// Historical turn (asstNode) should compact observation and preserve receipt ID
	u2, _ := mgr.CreateNode(asstNode.ID, domain.RoleUser, "What was in the receipt?", false)
	messages, err := mgr.BuildLLMContext(u2.ID, false)
	if err != nil {
		t.Fatalf("BuildLLMContext failed: %v", err)
	}

	foundReceiptInContext := false
	for _, m := range messages {
		if strings.Contains(m.Content, expectedReceiptID) {
			foundReceiptInContext = true
			break
		}
	}
	if !foundReceiptInContext {
		t.Errorf("expected receipt ID %s to be preserved in historical prompt context", expectedReceiptID)
	}
}
