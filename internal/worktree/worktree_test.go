package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestGitRepo(t *testing.T) (repoDir string, configDir string) {
	t.Helper()

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found, skipping git worktree tests")
	}

	repoDir = evalDir(t, t.TempDir())
	configDir = evalDir(t, t.TempDir())

	// Initialize git repository
	runGitCmd(t, gitPath, repoDir, "init", "-b", "main")
	runGitCmd(t, gitPath, repoDir, "config", "user.name", "Please Test")
	runGitCmd(t, gitPath, repoDir, "config", "user.email", "test@please.dev")

	// Commit initial file
	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	runGitCmd(t, gitPath, repoDir, "add", "README.md")
	runGitCmd(t, gitPath, repoDir, "commit", "-m", "Initial commit")

	return repoDir, configDir
}

func evalDir(t *testing.T, dir string) string {
	t.Helper()
	eval, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return eval
}

func runGitCmd(t *testing.T, gitPath, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitPath, append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\nOutput: %s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestWorktree_NonGitRepoFallback(t *testing.T) {
	nonRepoDir := evalDir(t, t.TempDir())
	configDir := evalDir(t, t.TempDir())

	mgr := NewManager(configDir, nonRepoDir)
	if mgr.IsGitRepo() {
		t.Errorf("expected IsGitRepo to be false for non-git dir")
	}

	wtDir, branch, err := mgr.EnsureWorktree("alpha")
	if err != ErrNotGitRepo {
		t.Errorf("expected ErrNotGitRepo, got %v", err)
	}
	if wtDir != nonRepoDir {
		t.Errorf("expected fallback to nonRepoDir, got %s", wtDir)
	}
	if branch != "" {
		t.Errorf("expected empty branch on fallback, got %s", branch)
	}
}

func TestWorktree_MainSessionReturnsPrimary(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)

	if !mgr.IsGitRepo() {
		t.Fatalf("expected IsGitRepo to be true")
	}

	wtDir, branch, err := mgr.EnsureWorktree("main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wtDir != repoDir {
		t.Errorf("expected wtDir to equal repoDir %s, got %s", repoDir, wtDir)
	}
	if branch != "main" {
		t.Errorf("expected branch 'main', got '%s'", branch)
	}
}

func TestWorktree_LifecycleAndIsolation(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)

	// 1. Ensure worktree for "alpha"
	alphaDir, alphaBranch, err := mgr.EnsureWorktree("alpha")
	if err != nil {
		t.Fatalf("failed to ensure worktree for alpha: %v", err)
	}

	if alphaBranch != "please/alpha" {
		t.Errorf("expected branch 'please/alpha', got '%s'", alphaBranch)
	}

	// Verify alphaDir is out-of-tree inside configDir
	if !strings.HasPrefix(alphaDir, configDir) {
		t.Errorf("expected alphaDir to be inside configDir %s, got %s", configDir, alphaDir)
	}

	// Verify README.md exists in alphaDir
	alphaReadme := filepath.Join(alphaDir, "README.md")
	if _, err := os.Stat(alphaReadme); err != nil {
		t.Errorf("expected README.md in alphaDir: %v", err)
	}

	// 2. Test Isolation: Write file only in alphaDir
	alphaSecret := filepath.Join(alphaDir, "alpha_only.txt")
	if err := os.WriteFile(alphaSecret, []byte("alpha secret"), 0644); err != nil {
		t.Fatalf("failed to write alpha_only.txt: %v", err)
	}

	primarySecret := filepath.Join(repoDir, "alpha_only.txt")
	if _, err := os.Stat(primarySecret); !os.IsNotExist(err) {
		t.Errorf("alpha_only.txt leaked into primary repository!")
	}

	// 3. Idempotency: Call EnsureWorktree again for "alpha"
	alphaDir2, alphaBranch2, err := mgr.EnsureWorktree("alpha")
	if err != nil {
		t.Fatalf("second EnsureWorktree failed: %v", err)
	}
	if alphaDir2 != alphaDir || alphaBranch2 != alphaBranch {
		t.Errorf("idempotent call mismatch: got (%s, %s), expected (%s, %s)", alphaDir2, alphaBranch2, alphaDir, alphaBranch)
	}

	// 4. List worktrees
	list, err := mgr.ListWorktrees()
	if err != nil {
		t.Fatalf("ListWorktrees failed: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 worktrees (primary + alpha), got %d", len(list))
	}

	foundAlpha := false
	for _, wt := range list {
		if wt.SessionID == "alpha" && wt.Branch == "please/alpha" {
			foundAlpha = true
			if !wt.IsActive {
				t.Errorf("expected alpha worktree to be active")
			}
			if wt.IsPrimary {
				t.Errorf("expected alpha worktree not to be primary")
			}
		}
	}
	if !foundAlpha {
		t.Errorf("alpha worktree not found in ListWorktrees: %+v", list)
	}

	// 5. Remove worktree
	if err := mgr.RemoveWorktree("alpha", true, true); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}

	// Verify alpha directory was removed
	if _, err := os.Stat(alphaDir); !os.IsNotExist(err) {
		t.Errorf("expected alphaDir to be removed, still exists")
	}

	// Verify main session protection
	if err := mgr.RemoveWorktree("main", true, true); err != ErrMainSessionProtected {
		t.Errorf("expected ErrMainSessionProtected when removing main, got %v", err)
	}
}
