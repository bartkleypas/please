package engine

import (
	"os"
	"strings"
	"testing"
)

func TestDeriveAmbientTelemetry(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}

	// 1. Basic telemetry derivation
	telem := DeriveAmbientTelemetry(wd, nil)
	if !strings.Contains(telem, "cwd: ") {
		t.Errorf("expected telemetry to contain cwd, got: %s", telem)
	}
	if !strings.Contains(telem, "local_time: ") {
		t.Errorf("expected telemetry to contain local_time, got: %s", telem)
	}

	// 2. With client context
	clientCtx := map[string]string{
		"active_file": "/Users/bart/Code/please/internal/engine/telemetry.go",
		"cursor_line": "42",
	}
	telemWithClient := DeriveAmbientTelemetry(wd, clientCtx)
	if !strings.Contains(telemWithClient, "active_file: /Users/bart/Code/please/internal/engine/telemetry.go") {
		t.Errorf("expected telemetry to contain active_file, got: %s", telemWithClient)
	}
	if !strings.Contains(telemWithClient, "cursor_line: 42") {
		t.Errorf("expected telemetry to contain cursor_line, got: %s", telemWithClient)
	}

	// 3. Strict bounding check: telemetry block must be compact (<256 bytes)
	if len(telemWithClient) > 256 {
		t.Errorf("expected telemetry payload to be <256 bytes, got %d bytes: %s", len(telemWithClient), telemWithClient)
	}
}

func TestFormatTelemetryEnvelope(t *testing.T) {
	prompt := "What is the capital of France?"
	telem := "cwd: /tmp\nlocal_time: 2026-09-05T08:00:00-07:00"

	// 1. Full envelope with telemetry
	enveloped := FormatTelemetryEnvelope(prompt, telem)
	expected := "<USER_REQUEST>\nWhat is the capital of France?\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\ncwd: /tmp\nlocal_time: 2026-09-05T08:00:00-07:00\n</ADDITIONAL_METADATA>"
	if enveloped != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, enveloped)
	}

	// 2. Empty telemetry envelope
	emptyEnveloped := FormatTelemetryEnvelope(prompt, "")
	expectedEmpty := "<USER_REQUEST>\nWhat is the capital of France?\n</USER_REQUEST>"
	if emptyEnveloped != expectedEmpty {
		t.Errorf("expected:\n%s\ngot:\n%s", expectedEmpty, emptyEnveloped)
	}
}

func TestFindWorkspaceIndex(t *testing.T) {
	// 1. Directory with index.md
	tmpDir1 := t.TempDir()
	_ = os.WriteFile(tmpDir1+"/index.md", []byte("# Root Index"), 0644)
	if idx := findWorkspaceIndex(tmpDir1); idx != "index.md" {
		t.Errorf("expected 'index.md', got %q", idx)
	}

	// Verify DeriveAmbientTelemetry includes it
	telem1 := DeriveAmbientTelemetry(tmpDir1, nil)
	if !strings.Contains(telem1, "index_file: index.md") {
		t.Errorf("expected telemetry to include 'index_file: index.md', got:\n%s", telem1)
	}

	// 2. Directory with README.md only
	tmpDir2 := t.TempDir()
	_ = os.WriteFile(tmpDir2+"/README.md", []byte("# Readme"), 0644)
	if idx := findWorkspaceIndex(tmpDir2); idx != "README.md" {
		t.Errorf("expected 'README.md', got %q", idx)
	}

	// 3. Directory with no index
	tmpDir3 := t.TempDir()
	if idx := findWorkspaceIndex(tmpDir3); idx != "" {
		t.Errorf("expected empty string, got %q", idx)
	}
}
