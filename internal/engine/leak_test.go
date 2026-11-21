package engine

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
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

func TestNoGoroutineLeaks_SubagentSpawnAndCancel(t *testing.T) {
	baseline, _ := countGoroutineLeaks(t)

	wsDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", t.TempDir())

	sqlStore, err := storage.NewSQLiteStorage(filepath.Join(t.TempDir(), "vault.db"), "")
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	mgr := NewManager(graph.NewGraph(), sqlStore)
	mgr.WorkspaceDir = wsDir
	mgr.RegisterDefaultTools(wsDir)
	cfg := config.NewDefaultConfig()

	// 1. Subagent run to normal completion.
	completedProv := &providers.MockLLMProvider{
		ResponseContent: "subagent done",
	}
	orchestrator := NewSubagentOrchestrator(mgr, completedProv, cfg)
	res, err := orchestrator.SpawnSubagent(context.Background(), tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Inspect workspace",
		SessionLabel:    "leak-complete",
		ToolPreset:      "read_only",
		IsolateWorktree: false,
		MaxSteps:        3,
	})
	if err != nil {
		t.Fatalf("SpawnSubagent (complete) failed: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected status 'completed', got %q", res.Status)
	}

	// 2. Subagent run cancelled mid-flight via parent context cancellation.
	slowProv := &providers.MockLLMProvider{
		Delay: 5 * time.Second,
	}
	orchestrator2 := NewSubagentOrchestrator(mgr, slowProv, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(25*time.Millisecond, cancel)
	defer timer.Stop()

	res2, err := orchestrator2.SpawnSubagent(ctx, tools.SubagentRequest{
		ParentSessionID: "main",
		Task:            "Slow audit",
		SessionLabel:    "leak-cancel",
		ToolPreset:      "read_only",
		IsolateWorktree: false,
		MaxSteps:        3,
	})
	cancel()
	if err != nil {
		t.Fatalf("SpawnSubagent (cancel) returned unexpected error: %v", err)
	}
	if res2.Status != "cancelled" {
		t.Fatalf("expected status 'cancelled', got %q", res2.Status)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("mgr.Close failed: %v", err)
	}

	assertNoNewGoroutineLeaks(t, baseline)
}
