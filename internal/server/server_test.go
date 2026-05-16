package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bartkleypas/please/internal/engine"
)

func TestServerAutoSync(t *testing.T) {
	// 1. Setup temporary storage
	tmpFile, err := os.CreateTemp("", "vault-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	storage, err := engine.NewSQLiteStorage(tmpFile.Name(), "")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)
	srv := NewServer(mgr)

	// 2. Create an initial node
	node, err := mgr.CreateNode("", engine.RoleSystem, "Initial Prompt", false)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	// 3. Simulate an external process adding a node directly to the DB
	externalStorage, _ := engine.NewSQLiteStorage(tmpFile.Name(), "")
	externalNode := &engine.Node{
		ID:       "external-node",
		ParentID: node.ID,
		Role:     engine.RoleUser,
		Content:  "External Message",
	}
	if err := externalStorage.SaveNode(externalNode); err != nil {
		t.Fatalf("failed to save external node: %v", err)
	}

	// 4. Call the server API
	req := httptest.NewRequest("GET", "/api/graph", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", w.Code)
	}

	// 5. Verify the external node is present in the response
	var respGraph engine.Graph
	if err := json.NewDecoder(w.Body).Decode(&respGraph); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := respGraph.Nodes["external-node"]; !ok {
		t.Error("Auto-Sync failed: external-node not found in server response")
	}
}
