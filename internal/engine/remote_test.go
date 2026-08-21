package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	if thoughtReceived != "Thinking about life..." {
		t.Errorf("expected thought 'Thinking about life...', got '%s'", thoughtReceived)
	}
	if contentReceived != "Hello World!" {
		t.Errorf("expected content 'Hello World!', got '%s'", contentReceived)
	}
	if len(toolCallsReceived) != 1 || toolCallsReceived[0].Function.Name != "read_file" {
		t.Errorf("expected 1 tool call to read_file, got %+v", toolCallsReceived)
	}
}
