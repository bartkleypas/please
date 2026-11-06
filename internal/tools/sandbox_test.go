package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSafePath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "please_sandbox_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}

	testFile := filepath.Join(subDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	tests := []struct {
		name      string
		wsDir     string
		path      string
		wantError bool
	}{
		{
			name:      "Valid relative path",
			wsDir:     tempDir,
			path:      "sub/file.txt",
			wantError: false,
		},
		{
			name:      "Valid absolute path inside workspace",
			wsDir:     tempDir,
			path:      testFile,
			wantError: false,
		},
		{
			name:      "Lexical traversal outside workspace",
			wsDir:     tempDir,
			path:      "../../etc/passwd",
			wantError: true,
		},
		{
			name:      "Absolute path outside workspace",
			wsDir:     tempDir,
			path:      "/etc/passwd",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateSafePath(tt.wsDir, tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateSafePath(%q, %q) error = %v, wantError = %v", tt.wsDir, tt.path, err, tt.wantError)
			}
		})
	}
}

func TestValidateSafePath_Quarantine(t *testing.T) {
	wsDir := t.TempDir()

	// Create benign and sensitive files in the workspace
	_ = os.WriteFile(filepath.Join(wsDir, "main.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "environment.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, ".env"), []byte("SECRET=1"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, ".env.local"), []byte("SECRET=2"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "vault.db"), []byte("sqlite"), 0644)

	secretsDir := filepath.Join(wsDir, ".secrets")
	_ = os.MkdirAll(secretsDir, 0755)
	_ = os.WriteFile(filepath.Join(secretsDir, "key.pem"), []byte("secret"), 0600)

	sshDir := filepath.Join(wsDir, ".ssh")
	_ = os.MkdirAll(sshDir, 0755)
	_ = os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("key"), 0600)

	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{"Benign Go file", "main.go", false},
		{"Benign file with 'env' in name", "environment.go", false},
		{"Quarantined .secrets directory", ".secrets/key.pem", true},
		{"Quarantined .env file", ".env", true},
		{"Quarantined .env.local file", ".env.local", true},
		{"Quarantined .ssh private key", ".ssh/id_ed25519", true},
		{"Quarantined standalone private key name", "id_ed25519", true},
		{"Quarantined vault database", "vault.db", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateSafePath(wsDir, tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateSafePath(%q) err = %v, wantError = %v", tt.path, err, tt.wantError)
			}
		})
	}
}

func TestValidateSafePath_SymlinkEscape(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "please_sandbox_symlink_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outsideDir, err := os.MkdirTemp("", "please_outside_*")
	if err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)

	outsideSecret := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to write outside secret: %v", err)
	}

	// Create a symlink inside workspace pointing outside
	symlinkPath := filepath.Join(tempDir, "link_outside")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Attempting to access file through symlink pointing outside must be caught
	_, err = ValidateSafePath(tempDir, "link_outside/secret.txt")
	if err == nil {
		t.Errorf("expected security error when accessing path through escaping symlink, got nil")
	}
}

func TestValidateSafePath_WorktreeVirtualization(t *testing.T) {
	primaryDir := t.TempDir()
	worktreeDir := t.TempDir()

	// An absolute path referencing the primary workspace
	primaryFile := filepath.Join(primaryDir, "cmd", "main.go")

	// ValidateSafePath with primaryWorkspace provided should rebase it onto worktreeDir
	resolved, err := ValidateSafePath(worktreeDir, primaryFile, primaryDir)
	if err != nil {
		t.Fatalf("unexpected error virtualizing path: %v", err)
	}

	expected := canonicalizePath(filepath.Join(worktreeDir, "cmd", "main.go"))
	if resolved != expected {
		t.Errorf("expected virtualized path %s, got %s", expected, resolved)
	}
}

func TestToolDefaults_WorkspaceScoping(t *testing.T) {
	wsDir := t.TempDir()
	absWs, _ := filepath.Abs(wsDir)

	registry := NewToolRegistry()
	RegisterDefaultTools(registry, wsDir)

	tools := registry.GetTools()
	toolsMap := make(map[string]Tool)
	for _, tool := range tools {
		toolsMap[tool.Name] = tool
	}

	ctx := context.Background()

	// 1. Test write_file in workspace
	writeTool, ok := toolsMap["write_file"]
	if !ok {
		t.Fatal("write_file tool not found")
	}
	_, err := writeTool.Function(ctx, map[string]interface{}{
		"path":    "hello.txt",
		"content": "Hello Workspace",
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	// Verify file was written inside wsDir
	writtenBytes, err := os.ReadFile(filepath.Join(absWs, "hello.txt"))
	if err != nil || string(writtenBytes) != "Hello Workspace" {
		t.Fatalf("file not found in workspace: %v, content: %s", err, string(writtenBytes))
	}

	// 2. Test read_file in workspace
	readTool, ok := toolsMap["read_file"]
	if !ok {
		t.Fatal("read_file tool not found")
	}
	content, err := readTool.Function(ctx, map[string]interface{}{
		"path": "hello.txt",
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if !strings.Contains(content, "Hello Workspace") {
		t.Errorf("expected content to contain 'Hello Workspace', got '%s'", content)
	}

	// 3. Test security sandbox: Path traversal rejected
	_, err = readTool.Function(ctx, map[string]interface{}{
		"path": "../../../etc/passwd",
	})
	if err == nil {
		t.Fatal("expected path traversal outside workspace to fail with security error")
	}
	if !strings.Contains(err.Error(), "outside of workspace root") && !strings.Contains(err.Error(), "outside of project root") {
		t.Errorf("expected security error, got: %v", err)
	}

	// 4. Test execute_command runs in workspace dir
	cmdTool, ok := toolsMap["execute_command"]
	if !ok {
		t.Fatal("execute_command tool not found")
	}
	pwdOut, err := cmdTool.Function(ctx, map[string]interface{}{
		"command": "pwd",
	})
	if err != nil {
		t.Fatalf("execute_command pwd failed: %v", err)
	}
	if strings.TrimSpace(pwdOut) != absWs {
		t.Errorf("expected execute_command to run in %s, got %s", absWs, strings.TrimSpace(pwdOut))
	}
}

func TestArgumentHelpers(t *testing.T) {
	args := map[string]interface{}{
		"int_direct":       25,
		"float_direct":     25.0,
		"str_int":          "25",
		"str_int_padded":   "  25  ",
		"str_float":        "25.0",
		"bool_direct":      true,
		"bool_str_true":    "true",
		"bool_str_1":       "1",
		"bool_str_yes":     "yes",
		"bool_str_false":   "false",
		"bool_str_false_p": " False ",
		"bool_str_0":       "0",
		"float_num":        0.85,
		"float_str":        " 0.95 ",
	}

	// Test getIntArg
	if got := getIntArg(args, "int_direct", 10); got != 25 {
		t.Errorf("expected 25, got %d", got)
	}
	if got := getIntArg(args, "float_direct", 10); got != 25 {
		t.Errorf("expected 25, got %d", got)
	}
	if got := getIntArg(args, "str_int", 10); got != 25 {
		t.Errorf("expected 25 from string '25', got %d", got)
	}
	if got := getIntArg(args, "str_int_padded", 10); got != 25 {
		t.Errorf("expected 25 from padded string '  25  ', got %d", got)
	}
	if got := getIntArg(args, "str_float", 10); got != 25 {
		t.Errorf("expected 25 from string '25.0', got %d", got)
	}
	if got := getIntArg(args, "non_existent", 10); got != 10 {
		t.Errorf("expected default 10, got %d", got)
	}

	// Test getBoolArg
	if got := getBoolArg(args, "bool_direct", false); !got {
		t.Errorf("expected true, got false")
	}
	if got := getBoolArg(args, "bool_str_true", false); !got {
		t.Errorf("expected true from string 'true', got false")
	}
	if got := getBoolArg(args, "bool_str_1", false); !got {
		t.Errorf("expected true from string '1', got false")
	}
	if got := getBoolArg(args, "bool_str_yes", false); !got {
		t.Errorf("expected true from string 'yes', got false")
	}
	if got := getBoolArg(args, "bool_str_false", true); got {
		t.Errorf("expected false from string 'false', got true")
	}
	if got := getBoolArg(args, "bool_str_false_p", true); got {
		t.Errorf("expected false from padded string ' False ', got true")
	}
	if got := getBoolArg(args, "bool_str_0", true); got {
		t.Errorf("expected false from string '0', got true")
	}
	if got := getBoolArg(args, "non_existent", true); !got {
		t.Errorf("expected default true, got false")
	}

	// Test getFloatArg
	if got := getFloatArg(args, "float_num", 1.0); got != 0.85 {
		t.Errorf("expected 0.85, got %f", got)
	}
	if got := getFloatArg(args, "float_str", 1.0); got != 0.95 {
		t.Errorf("expected 0.95, got %f", got)
	}
	if got := getFloatArg(args, "non_existent", 1.0); got != 1.0 {
		t.Errorf("expected default 1.0, got %f", got)
	}
}
