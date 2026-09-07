package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteDaemonStorage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/nodes":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{"id":"node-123","status":"saved"}`)
				return
			}
		case "/api/v1/graph":
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"nodes":{"node-123":{"id":"node-123","role":"user","content":"hello"}}}`)
				return
			}
		case "/api/v1/gc":
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"deleted_nodes":5}`)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	storage, err := NewRemoteDaemonStorage(ts.URL, "test-token", "")
	if err != nil {
		t.Fatalf("failed to create remote storage: %v", err)
	}

	// 1. Test SaveNode
	node := &Node{ID: "node-123", Role: RoleUser, Content: "hello"}
	if err := storage.SaveNode(node); err != nil {
		t.Fatalf("SaveNode failed: %v", err)
	}

	// 2. Test LoadGraph
	graph, latestID, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(graph.Nodes))
	}
	if latestID != "node-123" {
		t.Errorf("expected latest ID 'node-123', got '%s'", latestID)
	}

	// 3. Test GarbageCollect
	deleted, err := storage.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect failed: %v", err)
	}
	if deleted != 5 {
		t.Errorf("expected 5 deleted nodes, got %d", deleted)
	}
}
