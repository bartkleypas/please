package tools

import (
	"os"
	"path/filepath"
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
