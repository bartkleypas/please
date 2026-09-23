package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/domain"
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
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "Say hello!"},
	}
	contentChan, thoughtChan, toolCallChan, errChan := provider.GenerateResponseStream(context.Background(), messages, nil)

	var thoughtReceived string
	var contentReceived string
	var toolCallsReceived []domain.ToolCall

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

func TestResolveCACert(t *testing.T) {
	// Explicit path should be preserved
	if res := ResolveCACert("/custom/ca.crt", "https://localhost:8080"); res != "/custom/ca.crt" {
		t.Errorf("expected '/custom/ca.crt', got '%s'", res)
	}

	// Non-HTTPS should return empty if no cert path provided
	if res := ResolveCACert("", "http://localhost:8080"); res != "" {
		t.Errorf("expected empty cert for http, got '%s'", res)
	}
}

func TestRemoteDaemonProvider_SessionIDHeader(t *testing.T) {
	var receivedSessionID string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("X-Please-Session-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: token\ndata: {\"chunk\":\"test\"}\n\n")
	}))
	defer ts.Close()

	provider, err := NewRemoteDaemonProvider(ts.URL, "test-token", "")
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.SessionID == "" {
		t.Error("expected NewRemoteDaemonProvider to generate non-empty SessionID")
	}

	messages := []domain.Message{{Role: domain.RoleUser, Content: "ping"}}
	contentChan, _, _, errChan := provider.GenerateResponseStream(context.Background(), messages, nil)
	for contentChan != nil || errChan != nil {
		select {
		case _, ok := <-contentChan:
			if !ok {
				contentChan = nil
			}
		case _, ok := <-errChan:
			if !ok {
				errChan = nil
			}
		}
	}

	if receivedSessionID != provider.SessionID {
		t.Errorf("expected daemon to receive session ID '%s', got '%s'", provider.SessionID, receivedSessionID)
	}
}
