package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/engine"
)

func TestEventBus_PubSub(t *testing.T) {
	bus := NewEventBus()
	sub1 := bus.Subscribe()
	defer sub1.Close()
	sub2 := bus.Subscribe()
	defer sub2.Close()

	if bus.SubscriberCount() != 2 {
		t.Errorf("expected 2 subscribers, got %d", bus.SubscriberCount())
	}

	bus.Publish(EventNodeSaved, map[string]interface{}{"node_id": "n1"})

	select {
	case ev := <-sub1.Events:
		if ev.Type != EventNodeSaved || ev.Payload["node_id"] != "n1" {
			t.Errorf("sub1 unexpected event: %+v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for sub1 event")
	}

	select {
	case ev := <-sub2.Events:
		if ev.Type != EventNodeSaved || ev.Payload["node_id"] != "n1" {
			t.Errorf("sub2 unexpected event: %+v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for sub2 event")
	}

	// Test unsubscribe
	sub1.Close()
	if bus.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber after sub1 close, got %d", bus.SubscriberCount())
	}
}

func TestEventsEndpoint_SSE(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := engine.NewSQLiteStorage(tmpDir+"/vault.db", "")
	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)
	srv := NewServer(mgr)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to SSE stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	// Trigger node creation via REST API
	go func() {
		time.Sleep(50 * time.Millisecond)
		nodePayload := map[string]interface{}{
			"role":    "user",
			"content": "Live event test message",
		}
		jsonBytes, _ := json.Marshal(nodePayload)
		_, _ = http.Post(ts.URL+"/api/v1/nodes", "application/json", bytes.NewReader(jsonBytes))
	}()

	scanner := bufio.NewScanner(resp.Body)
	receivedEvent := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: node_saved") {
			receivedEvent = true
			break
		}
	}

	if !receivedEvent {
		t.Errorf("did not receive node_saved SSE event")
	}
}
