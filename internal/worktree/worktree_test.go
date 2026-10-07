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

func TestWorktree_SubsessionAndInspectChanges(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)

	subID := "sub_1234abcd"
	branch := "subsession/" + subID
	wtDir, actBranch, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}
	if actBranch != branch {
		t.Errorf("expected branch %s, got %s", branch, actBranch)
	}

	baseCommit, err := mgr.GetHeadCommit(wtDir)
	if err != nil || baseCommit == "" {
		t.Fatalf("GetHeadCommit failed: %v, commit=%s", err, baseCommit)
	}

	// Clean state inspection
	hasChanges, modFiles, err := mgr.InspectChanges(wtDir, baseCommit)
	if err != nil {
		t.Fatalf("InspectChanges failed: %v", err)
	}
	if hasChanges || len(modFiles) > 0 {
		t.Errorf("expected no changes on fresh worktree, got hasChanges=%v files=%v", hasChanges, modFiles)
	}

	// Modify a file in the worktree
	subFile := filepath.Join(wtDir, "sub_task.txt")
	if err := os.WriteFile(subFile, []byte("subagent work"), 0644); err != nil {
		t.Fatalf("failed to write sub_task.txt: %v", err)
	}

	hasChanges, modFiles, err = mgr.InspectChanges(wtDir, baseCommit)
	if err != nil {
		t.Fatalf("InspectChanges failed: %v", err)
	}
	if !hasChanges || len(modFiles) != 1 || modFiles[0] != "sub_task.txt" {
		t.Errorf("expected 1 modified file (sub_task.txt), got hasChanges=%v files=%v", hasChanges, modFiles)
	}

	// Commit the file in worktree
	gitPath, _ := exec.LookPath("git")
	runGitCmd(t, gitPath, wtDir, "add", "sub_task.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "subagent task complete")

	newCommit, _ := mgr.GetHeadCommit(wtDir)
	if newCommit == baseCommit {
		t.Errorf("expected new commit after git commit")
	}

	hasChanges, modFiles, err = mgr.InspectChanges(wtDir, baseCommit)
	if !hasChanges || len(modFiles) != 1 {
		t.Errorf("expected committed changes detected against baseCommit")
	}

	diffStat, err := mgr.GetDiffStat(wtDir, baseCommit)
	if err != nil || !strings.Contains(diffStat, "sub_task.txt") {
		t.Errorf("expected diffStat containing sub_task.txt, got %q (err=%v)", diffStat, err)
	}

	// Clean up worktree and branch
	if err := mgr.RemoveWorktree(subID, true, true); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
}

func TestWorktree_ReconcileBranch_Squash(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)
	gitPath, _ := exec.LookPath("git")

	subID := "sub_reconcile_squash"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	// Add file in subagent worktree
	taskFile := filepath.Join(wtDir, "feature.txt")
	if err := os.WriteFile(taskFile, []byte("feature work"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	runGitCmd(t, gitPath, wtDir, "add", "feature.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "implement feature")

	// Reconcile via squash with cleanup
	res, err := mgr.ReconcileBranch(branch, "squash", true, subID)
	if err != nil {
		t.Fatalf("ReconcileBranch squash failed: %v", err)
	}

	if res.Strategy != "squash" {
		t.Errorf("expected strategy squash, got %s", res.Strategy)
	}
	if res.Commit == "" {
		t.Errorf("expected non-empty commit SHA")
	}
	if len(res.FilesMerged) != 1 || res.FilesMerged[0] != "feature.txt" {
		t.Errorf("expected feature.txt in files merged, got: %v", res.FilesMerged)
	}

	// Verify primary repo has the file
	primaryFeature := filepath.Join(repoDir, "feature.txt")
	if data, err := os.ReadFile(primaryFeature); err != nil || string(data) != "feature work" {
		t.Errorf("primary repo missing merged content: %v, data=%s", err, string(data))
	}

	// Verify worktree directory is removed
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("expected worktree directory to be removed after cleanup")
	}

	// Verify branch is deleted
	if mgr.branchExists(repoDir, branch) {
		t.Errorf("expected branch %s to be deleted", branch)
	}
}

func TestWorktree_ReconcileBranch_Merge(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)
	gitPath, _ := exec.LookPath("git")

	subID := "sub_reconcile_merge"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	taskFile := filepath.Join(wtDir, "merge_test.txt")
	if err := os.WriteFile(taskFile, []byte("merge content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	runGitCmd(t, gitPath, wtDir, "add", "merge_test.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "merge test commit")

	// Reconcile via merge without cleanup
	res, err := mgr.ReconcileBranch(branch, "merge", false, subID)
	if err != nil {
		t.Fatalf("ReconcileBranch merge failed: %v", err)
	}

	if res.Strategy != "merge" {
		t.Errorf("expected strategy merge, got %s", res.Strategy)
	}
	if res.Commit == "" {
		t.Errorf("expected non-empty commit SHA")
	}
	if len(res.FilesMerged) != 1 || res.FilesMerged[0] != "merge_test.txt" {
		t.Errorf("expected merge_test.txt in files merged, got: %v", res.FilesMerged)
	}

	// Verify worktree still exists (cleanupWorktree=false)
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Errorf("expected worktree directory to still exist when cleanupWorktree=false")
	}

	// Clean up explicitly
	_ = mgr.RemoveWorktree(subID, true, true)
}

func TestWorktree_ReconcileBranch_CherryPick(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)
	gitPath, _ := exec.LookPath("git")

	subID := "sub_reconcile_cp"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	taskFile := filepath.Join(wtDir, "cherry.txt")
	if err := os.WriteFile(taskFile, []byte("cherry pick content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	runGitCmd(t, gitPath, wtDir, "add", "cherry.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "cherry commit")

	res, err := mgr.ReconcileBranch(branch, "cherry_pick", true, subID)
	if err != nil {
		t.Fatalf("ReconcileBranch cherry_pick failed: %v", err)
	}

	if res.Strategy != "cherry_pick" {
		t.Errorf("expected strategy cherry_pick, got %s", res.Strategy)
	}
	if res.Commit == "" {
		t.Errorf("expected non-empty commit SHA")
	}

	primaryCherry := filepath.Join(repoDir, "cherry.txt")
	if data, err := os.ReadFile(primaryCherry); err != nil || string(data) != "cherry pick content" {
		t.Errorf("primary repo missing cherry picked content: %v", err)
	}
}

func TestWorktree_ReconcileBranch_Discard(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)
	gitPath, _ := exec.LookPath("git")

	subID := "sub_reconcile_discard"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	taskFile := filepath.Join(wtDir, "discard_me.txt")
	if err := os.WriteFile(taskFile, []byte("will be discarded"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	runGitCmd(t, gitPath, wtDir, "add", "discard_me.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "discard commit")

	res, err := mgr.ReconcileBranch(branch, "discard", true, subID)
	if err != nil {
		t.Fatalf("ReconcileBranch discard failed: %v", err)
	}

	if res.Strategy != "discard" {
		t.Errorf("expected strategy discard, got %s", res.Strategy)
	}

	// Verify file is NOT in primary
	primaryFile := filepath.Join(repoDir, "discard_me.txt")
	if _, err := os.Stat(primaryFile); !os.IsNotExist(err) {
		t.Errorf("discarded file found in primary workspace!")
	}

	// Verify worktree is removed
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("expected worktree directory to be removed on discard")
	}

	// Verify branch is deleted
	if mgr.branchExists(repoDir, branch) {
		t.Errorf("expected branch %s to be deleted on discard", branch)
	}
}

func TestWorktree_ReconcileBranch_DirtyPrimaryValidation(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)
	gitPath, _ := exec.LookPath("git")

	subID := "sub_dirty_test"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	subFile := filepath.Join(wtDir, "sub_clean.txt")
	_ = os.WriteFile(subFile, []byte("sub content"), 0644)
	runGitCmd(t, gitPath, wtDir, "add", "sub_clean.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "sub commit")

	// Dirty the primary workspace with an uncommitted modification
	dirtyFile := filepath.Join(repoDir, "README.md")
	_ = os.WriteFile(dirtyFile, []byte("# Dirty Readme\n"), 0644)

	// Attempt reconcile: must fail with actionable error
	_, err = mgr.ReconcileBranch(branch, "squash", true, subID)
	if err == nil {
		t.Fatalf("expected error when reconciling into dirty primary workspace, got nil")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") && !strings.Contains(err.Error(), "clean") {
		t.Errorf("expected actionable error message about dirty workspace, got: %v", err)
	}

	// Clean up worktree
	_ = mgr.RemoveWorktree(subID, true, true)
}

func TestWorktree_GetDiffAndGetBranchDiff(t *testing.T) {
	repoDir, configDir := setupTestGitRepo(t)
	mgr := NewManager(configDir, repoDir)
	gitPath, _ := exec.LookPath("git")

	subID := "sub_diff_test"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch failed: %v", err)
	}

	baseCommit, _ := mgr.GetHeadCommit(wtDir)

	subFile := filepath.Join(wtDir, "diff_sample.txt")
	_ = os.WriteFile(subFile, []byte("diff line 1\n"), 0644)
	runGitCmd(t, gitPath, wtDir, "add", "diff_sample.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "add diff sample")

	diff, err := mgr.GetDiff(wtDir, baseCommit)
	if err != nil {
		t.Fatalf("GetDiff failed: %v", err)
	}
	if !strings.Contains(diff, "+diff line 1") {
		t.Errorf("expected diff to contain '+diff line 1', got: %s", diff)
	}

	bDiff, err := mgr.GetBranchDiff(branch)
	if err != nil {
		t.Fatalf("GetBranchDiff failed: %v", err)
	}
	if !strings.Contains(bDiff, "+diff line 1") {
		t.Errorf("expected branch diff to contain '+diff line 1', got: %s", bDiff)
	}

	_ = mgr.RemoveWorktree(subID, true, true)
}

func TestWorktree_NestedRepository(t *testing.T) {
	parentDir, configDir := setupTestGitRepo(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}

	nestedDir := filepath.Join(parentDir, "child_repo")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create child_repo dir: %v", err)
	}

	runGitCmd(t, gitPath, nestedDir, "init", "-b", "main")
	runGitCmd(t, gitPath, nestedDir, "config", "user.name", "Please Child Test")
	runGitCmd(t, gitPath, nestedDir, "config", "user.email", "child@please.dev")
	nestedReadme := filepath.Join(nestedDir, "README.child.md")
	if err := os.WriteFile(nestedReadme, []byte("# Child Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write nested readme: %v", err)
	}
	runGitCmd(t, gitPath, nestedDir, "add", "README.child.md")
	runGitCmd(t, gitPath, nestedDir, "commit", "-m", "Initial child commit")

	mgr := NewManager(configDir, nestedDir)
	if !mgr.IsGitRepo() {
		t.Fatal("expected nested repository to be recognized as git repo")
	}

	topLevel, err := mgr.GetTopLevel()
	if err != nil {
		t.Fatalf("GetTopLevel failed: %v", err)
	}
	evalNested := evalDir(t, nestedDir)
	if topLevel != evalNested {
		t.Errorf("expected topLevel %s, got %s", evalNested, topLevel)
	}

	subID := "sub_child_test"
	branch := "subsession/" + subID
	wtDir, _, err := mgr.EnsureWorktreeBranch(subID, branch)
	if err != nil {
		t.Fatalf("EnsureWorktreeBranch on nested repo failed: %v", err)
	}

	// Add file in child worktree
	childWorkFile := filepath.Join(wtDir, "nested_feature.txt")
	if err := os.WriteFile(childWorkFile, []byte("feature from child subagent\n"), 0644); err != nil {
		t.Fatalf("failed to write child work file: %v", err)
	}
	runGitCmd(t, gitPath, wtDir, "add", "nested_feature.txt")
	runGitCmd(t, gitPath, wtDir, "commit", "-m", "child feature commit")

	// Reconcile into child repo
	recRes, err := mgr.ReconcileBranch(branch, "squash", true, subID)
	if err != nil {
		t.Fatalf("ReconcileBranch failed: %v", err)
	}
	if recRes.Strategy != "squash" || recRes.Commit == "" {
		t.Errorf("unexpected ReconcileResult: %+v", recRes)
	}

	// File should exist in child_repo, NOT in parentDir root
	if _, err := os.Stat(filepath.Join(nestedDir, "nested_feature.txt")); err != nil {
		t.Errorf("nested_feature.txt missing in child_repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parentDir, "nested_feature.txt")); !os.IsNotExist(err) {
		t.Errorf("nested_feature.txt must NOT exist in parent workspace root")
	}
}


