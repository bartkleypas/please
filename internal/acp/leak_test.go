package acp

import (
	"bytes"
	"context"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/providers"
	acpsdk "github.com/coder/acp-go-sdk"
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

func TestNoGoroutineLeaks_ACPPromptAndCancel(t *testing.T) {
	baseline, _ := countGoroutineLeaks(t)

	// 1. Completed prompt turn and connection teardown.
	completedProv := &providers.MockLLMProvider{
		ResponseContent: "acp complete",
	}
	connClient, _, _, cleanup := setupConnectedHarness(t, "allow_once", completedProv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/workspace/demo",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		cleanup()
		t.Fatalf("NewSession failed: %v", err)
	}

	promptResp, err := connClient.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("Hello")},
	})
	if err != nil {
		cleanup()
		t.Fatalf("Prompt failed: %v", err)
	}
	if promptResp.StopReason != acpsdk.StopReasonEndTurn {
		cleanup()
		t.Fatalf("expected StopReasonEndTurn, got %q", promptResp.StopReason)
	}
	cleanup()

	// 2. Mid-flight prompt cancellation via ACP Cancel notification.
	slowProv := &providers.MockLLMProvider{
		Delay: 5 * time.Second,
	}
	connClient2, _, _, cleanup2 := setupConnectedHarness(t, "allow_once", slowProv)

	newResp2, err := connClient2.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/workspace/demo",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		cleanup2()
		t.Fatalf("NewSession (cancel case) failed: %v", err)
	}

	promptDone := make(chan error, 1)
	go func() {
		_, err := connClient2.Prompt(ctx, acpsdk.PromptRequest{
			SessionId: newResp2.SessionId,
			Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("Long turn")},
		})
		promptDone <- err
	}()

	time.Sleep(25 * time.Millisecond)
	if err := connClient2.Cancel(ctx, acpsdk.CancelNotification{
		SessionId: newResp2.SessionId,
	}); err != nil {
		cleanup2()
		t.Fatalf("Cancel notification failed: %v", err)
	}

	select {
	case <-promptDone:
	case <-time.After(2 * time.Second):
		cleanup2()
		t.Fatal("timed out waiting for cancelled Prompt to return")
	}
	cleanup2()

	assertNoNewGoroutineLeaks(t, baseline)
}
