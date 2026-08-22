package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteDaemonProvider_Stream(t *testing.T) {
	// Setup test SSE server simulating daemon output
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/stream" {
			http.NotFound(w, r)
			return
		}

		if r.Header.Get("Authorization") != "Bearer secret-test-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)

		// Emit thought event
		fmt.Fprintf(w, "event: thought\ndata: {\"chunk\":\"Thinking about life...\"}\n\n")
		flusher.Flush()

		// Emit token events
		fmt.Fprintf(w, "event: token\ndata: {\"chunk\":\"Hello \"}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "event: token\ndata: {\"chunk\":\"World!\"}\n\n")
		flusher.Flush()

		// Emit tool call
		fmt.Fprintf(w, "event: tool_call\ndata: {\"id\":\"call_1\",\"tool\":\"read_file\",\"arguments\":{\"path\":\"README.md\"}}\n\n")
		flusher.Flush()

		// Emit node_complete
		fmt.Fprintf(w, "event: node_complete\ndata: {\"node_id\":\"019...\",\"parent_id\":\"018...\"}\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	// 1. Create RemoteDaemonProvider
	provider, err := NewRemoteDaemonProvider(ts.URL, "secret-test-token", "")
	if err != nil {
		t.Fatalf("failed to create remote provider: %v", err)
	}

	// 2. Consume stream
	messages := []Message{
		{Role: RoleUser, Content: "Say hello!"},
	}
	contentChan, thoughtChan, toolCallChan, errChan := provider.GenerateResponseStream(context.Background(), messages, nil)

	var thoughtReceived string
	var contentReceived string
	var toolCallsReceived []ToolCall

	for contentChan != nil || thoughtChan != nil || toolCallChan != nil || errChan != nil {
		select {
		case thought, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
				continue
			}
			thoughtReceived += thought
		case chunk, ok := <-contentChan:
			if !ok {
				contentChan = nil
				continue
			}
			contentReceived += chunk
		case tc, ok := <-toolCallChan:
			if !ok {
				toolCallChan = nil
				continue
			}
			toolCallsReceived = append(toolCallsReceived, tc...)
		case streamErr, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if streamErr != nil {
				t.Fatalf("stream returned error: %v", streamErr)
			}
		}
	}

	if !strings.Contains(thoughtReceived, "Thinking about life...") {
		t.Errorf("expected thought to contain 'Thinking about life...', got '%s'", thoughtReceived)
	}
	if !strings.Contains(thoughtReceived, "read_file") {
		t.Errorf("expected thought to contain 'read_file', got '%s'", thoughtReceived)
	}
	if contentReceived != "Hello World!" {
		t.Errorf("expected content 'Hello World!', got '%s'", contentReceived)
	}
}

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
