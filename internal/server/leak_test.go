package server

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
)

func countGoroutineLeaks(t *testing.T) (int, string) {
	t.Helper()
	runtime.Gosched()
	time.Sleep(25 * time.Millisecond)

	prof := pprof.Lookup("goroutineleak")
	if prof == nil {
		t.Fatal("runtime/pprof goroutineleak profile unavailable")
	}

	var buf bytes.Buffer
	if err := prof.WriteTo(&buf, 1); err != nil {
		t.Fatalf("failed to write goroutineleak profile: %v", err)
	}

	text := buf.String()
	const prefix = "goroutineleak profile: total "
	firstLine, _, _ := strings.Cut(text, "\n")
	rest, ok := strings.CutPrefix(firstLine, prefix)
	if !ok {
		t.Fatalf("unexpected goroutineleak profile header: %q\n%s", firstLine, text)
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		t.Fatalf("missing goroutineleak count in header: %q\n%s", firstLine, text)
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("invalid goroutineleak count in header %q: %v", firstLine, err)
	}
	return count, text
}

func assertNoNewGoroutineLeaks(t *testing.T, baseline int) {
	t.Helper()
	after, text := countGoroutineLeaks(t)
	if after > baseline {
		t.Fatalf("detected %d new leaked goroutine(s) (baseline=%d, after=%d):\n%s", after-baseline, baseline, after, text)
	}
}

func TestNoGoroutineLeaks_SSEDisconnectAndSessionActor(t *testing.T) {
	baseline, _ := countGoroutineLeaks(t)

	tmpDir := t.TempDir()
	strg, err := storage.NewSQLiteStorage(filepath.Join(tmpDir, "leak.db"), "")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	mgr := engine.NewManager(graph.NewGraph(), strg)
	mockProvider := &providers.MockLLMProvider{
		ResponseContent: "leak check ok",
		ResponseThought: "thinking",
	}
	pacing := false
	cfg := &config.Config{
		Server: &config.ServerConfig{
			Provider: "mock",
			Model:    "mock-model",
		},
		Client: &config.ClientConfig{
			NaturalPacing: &pacing,
		},
	}

	srv := NewServerWithProvider(mgr, mockProvider, cfg)
	ts := httptest.NewServer(srv.Handler())
	client := &http.Client{}

	// 1. Connect and disconnect an SSE subscriber on /api/v1/events.
	sseCtx, sseCancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext failed: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events failed: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for srv.EventBus.SubscriberCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.EventBus.SubscriberCount() != 1 {
		t.Fatalf("expected 1 SSE subscriber, got %d", srv.EventBus.SubscriberCount())
	}

	srv.EventBus.Publish(EventNodeSaved, map[string]interface{}{"node_id": "leak-check"})
	scanner := bufio.NewScanner(resp.Body)
	gotEvent := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: node_saved") {
			gotEvent = true
			break
		}
	}
	if !gotEvent {
		t.Fatalf("did not receive node_saved SSE event (err=%v)", scanner.Err())
	}

	sseCancel()
	_ = resp.Body.Close()

	deadline = time.Now().Add(500 * time.Millisecond)
	for srv.EventBus.SubscriberCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.EventBus.SubscriberCount() != 0 {
		t.Fatalf("expected 0 SSE subscribers after disconnect, got %d", srv.EventBus.SubscriberCount())
	}

	// 2. Execute a streaming chat turn through SessionActor.
	chatResp, err := client.Post(ts.URL+"/api/v1/chat/stream", "application/json", strings.NewReader(`{"message":"hello","role":"user"}`))
	if err != nil {
		t.Fatalf("POST /api/v1/chat/stream failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, chatResp.Body)
	_ = chatResp.Body.Close()

	// 3. Tear down HTTP client/server, SessionActor workers, and SQLite pool, then check for leaks.
	client.CloseIdleConnections()
	ts.Close()
	if srv.Actors != nil {
		srv.Actors.StopAll()
	}
	if err := strg.Close(); err != nil {
		t.Fatalf("strg.Close failed: %v", err)
	}

	assertNoNewGoroutineLeaks(t, baseline)
}
