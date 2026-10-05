package worktree

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrGitNotAvailable indicates git executable was not found in PATH.
	ErrGitNotAvailable = errors.New("git binary not found in PATH")

	// ErrNotGitRepo indicates the target directory is not inside a git repository.
	ErrNotGitRepo = errors.New("workspace is not inside a git repository")

	// ErrMainSessionProtected indicates that the primary repo checkout cannot be deleted as a worktree.
	ErrMainSessionProtected = errors.New("cannot remove primary repository worktree for main session")
)

// WorktreeInfo contains metadata describing an active Git worktree.
type WorktreeInfo struct {
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	Commit    string `json:"commit"`
	SessionID string `json:"session_id,omitempty"`
	IsPrimary bool   `json:"is_primary"`
	IsActive  bool   `json:"is_active"`
}

// Manager coordinates Git worktree creation, tracking, and teardown
// for session sandboxing.
type Manager struct {
	configDir        string
	primaryWorkspace string
	gitPath          string
}

// NewManager creates an initialized Worktree Manager.
func NewManager(configDir, primaryWorkspace string) *Manager {
	if configDir == "" {
		if ucd, err := os.UserConfigDir(); err == nil {
			configDir = filepath.Join(ucd, "please")
		} else {
			configDir = filepath.Join(os.TempDir(), "please")
		}
	}

	absPrimary, err := filepath.Abs(primaryWorkspace)
	if err != nil {
		absPrimary = primaryWorkspace
	}
	if eval, err := filepath.EvalSymlinks(absPrimary); err == nil {
		absPrimary = eval
	}

	gitPath, _ := exec.LookPath("git")

	return &Manager{
		configDir:        configDir,
		primaryWorkspace: absPrimary,
		gitPath:          gitPath,
	}
}

// IsGitAvailable returns true if the git binary is found in PATH.
func (m *Manager) IsGitAvailable() bool {
	return m.gitPath != ""
}

// IsGitRepo verifies whether the primary workspace is inside a Git working tree.
func (m *Manager) IsGitRepo() bool {
	if !m.IsGitAvailable() {
		return false
	}
	cmd := exec.Command(m.gitPath, "-C", m.primaryWorkspace, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// GetTopLevel resolves the root directory of the primary Git repository checkout.
func (m *Manager) GetTopLevel() (string, error) {
	if !m.IsGitAvailable() {
		return "", ErrGitNotAvailable
	}
	cmd := exec.Command(m.gitPath, "-C", m.primaryWorkspace, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", ErrNotGitRepo
	}
	top := filepath.Clean(strings.TrimSpace(string(out)))
	if eval, err := filepath.EvalSymlinks(top); err == nil {
		top = eval
	}
	return top, nil
}

// GetRepoKey computes a deterministic, collision-resistant identifier for the repository.
// Format: <basename>-<sha256(topLevel)[:8]> (e.g. please-7a8b9c0d)
func (m *Manager) GetRepoKey() (string, error) {
	topLevel, err := m.GetTopLevel()
	if err != nil {
		return "", err
	}
	repoName := filepath.Base(topLevel)
	hash := sha256.Sum256([]byte(topLevel))
	return fmt.Sprintf("%s-%x", repoName, hash[:4]), nil
}

// GetWorktreeDir computes the out-of-tree directory path for a given session.
func (m *Manager) GetWorktreeDir(sessionID string) (string, error) {
	if sessionID == "" || sessionID == "main" {
		topLevel, err := m.GetTopLevel()
		if err != nil {
			return m.primaryWorkspace, nil
		}
		return topLevel, nil
	}

	repoKey, err := m.GetRepoKey()
	if err != nil {
		return "", err
	}

	return filepath.Join(m.configDir, "worktrees", repoKey, sessionID), nil
}

// EnsureWorktree provisions or resolves an isolated Git worktree for the given session.
// For session "main", it returns the primary repo root and current branch.
// For named sessions ("alpha", "beta"), it provisions an out-of-tree directory on branch 'please/<sessionID>'
// (or 'subsession/<sessionID>' if sessionID starts with 'sub_').
func (m *Manager) EnsureWorktree(sessionID string) (worktreeDir string, branch string, err error) {
	defaultBranch := fmt.Sprintf("please/%s", sessionID)
	if strings.HasPrefix(sessionID, "sub_") {
		defaultBranch = fmt.Sprintf("subsession/%s", sessionID)
	}
	return m.EnsureWorktreeBranch(sessionID, defaultBranch)
}

// EnsureWorktreeBranch provisions or resolves an isolated Git worktree for the given session on an explicit branch.
func (m *Manager) EnsureWorktreeBranch(sessionID, branch string) (worktreeDir string, actualBranch string, err error) {
	if !m.IsGitAvailable() {
		return m.primaryWorkspace, "", ErrGitNotAvailable
	}
	if !m.IsGitRepo() {
		return m.primaryWorkspace, "", ErrNotGitRepo
	}

	topLevel, err := m.GetTopLevel()
	if err != nil {
		return m.primaryWorkspace, "", err
	}

	// Main session uses the primary workspace root
	if sessionID == "" || sessionID == "main" {
		currentBranch := m.getCurrentBranch(topLevel)
		return topLevel, currentBranch, nil
	}

	if branch == "" {
		branch = fmt.Sprintf("please/%s", sessionID)
	}

	worktreeDir, err = m.GetWorktreeDir(sessionID)
	if err != nil {
		return m.primaryWorkspace, "", err
	}

	// Check if the worktree directory already exists and is valid
	if m.isWorktreeValid(worktreeDir) {
		return worktreeDir, branch, nil
	}

	// If directory exists but is not a valid git worktree, clean it up first
	if _, statErr := os.Stat(worktreeDir); statErr == nil {
		_ = os.RemoveAll(worktreeDir)
	}

	// Ensure parent directory exists
	if mkErr := os.MkdirAll(filepath.Dir(worktreeDir), 0755); mkErr != nil {
		return m.primaryWorkspace, "", fmt.Errorf("failed to create worktree parent directory: %w", mkErr)
	}

	// Check if the branch already exists
	branchExists := m.branchExists(topLevel, branch)

	var cmd *exec.Cmd
	if branchExists {
		// Attach worktree to existing branch
		cmd = exec.Command(m.gitPath, "-C", topLevel, "worktree", "add", worktreeDir, branch)
	} else {
		// Create new branch based off HEAD
		cmd = exec.Command(m.gitPath, "-C", topLevel, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if addErr := cmd.Run(); addErr != nil {
		return m.primaryWorkspace, "", fmt.Errorf("failed to add git worktree: %w (stderr: %s)", addErr, strings.TrimSpace(stderr.String()))
	}

	return worktreeDir, branch, nil
}

// RemoveWorktree deletes the worktree checkout and optionally deletes the git branch.
func (m *Manager) RemoveWorktree(sessionID string, force bool, deleteBranch bool) error {
	branch := fmt.Sprintf("please/%s", sessionID)
	if strings.HasPrefix(sessionID, "sub_") {
		branch = fmt.Sprintf("subsession/%s", sessionID)
	}
	return m.RemoveWorktreeBranch(sessionID, branch, force, deleteBranch)
}

// RemoveWorktreeBranch deletes the worktree checkout and optionally deletes the specified git branch.
func (m *Manager) RemoveWorktreeBranch(sessionID, branch string, force bool, deleteBranch bool) error {
	if sessionID == "" || sessionID == "main" {
		return ErrMainSessionProtected
	}

	if !m.IsGitAvailable() || !m.IsGitRepo() {
		return nil
	}

	topLevel, err := m.GetTopLevel()
	if err != nil {
		return err
	}

	worktreeDir, err := m.GetWorktreeDir(sessionID)
	if err != nil {
		return err
	}

	args := []string{"-C", topLevel, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreeDir)

	removeCmd := exec.Command(m.gitPath, args...)
	_ = removeCmd.Run()

	// Ensure directory is cleared if git worktree remove left artifacts
	_ = os.RemoveAll(worktreeDir)

	if deleteBranch && branch != "" {
		delBranchCmd := exec.Command(m.gitPath, "-C", topLevel, "branch", "-D", branch)
		_ = delBranchCmd.Run()
	}

	_ = m.Prune()
	return nil
}

// GetHeadCommit returns the current commit SHA of the worktree checkout.
func (m *Manager) GetHeadCommit(dir string) (string, error) {
	if !m.IsGitAvailable() {
		return "", ErrGitNotAvailable
	}
	cmd := exec.Command(m.gitPath, "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// InspectChanges checks for uncommitted modifications or new commits in the worktree directory.
// Returns whether changes exist, and a list of modified file paths relative to the worktree.
func (m *Manager) InspectChanges(dir string, baseCommit string) (hasChanges bool, filesModified []string, err error) {
	if !m.IsGitAvailable() {
		return false, nil, ErrGitNotAvailable
	}

	fileMap := make(map[string]bool)

	// 1. Check uncommitted changes via status --porcelain
	statusCmd := exec.Command(m.gitPath, "-C", dir, "status", "--porcelain")
	statusOut, _ := statusCmd.Output()
	lines := strings.Split(string(statusOut), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) > 3 {
			filePath := strings.TrimSpace(l[3:])
			if idx := strings.Index(filePath, " -> "); idx != -1 {
				filePath = filePath[idx+4:]
			}
			fileMap[filePath] = true
		}
	}

	// 2. Check committed changes if baseCommit is specified
	if baseCommit != "" {
		diffCmd := exec.Command(m.gitPath, "-C", dir, "diff", "--name-only", baseCommit, "HEAD")
		diffOut, _ := diffCmd.Output()
		dLines := strings.Split(string(diffOut), "\n")
		for _, dl := range dLines {
			dl = strings.TrimSpace(dl)
			if dl != "" {
				fileMap[dl] = true
			}
		}
	}

	for f := range fileMap {
		filesModified = append(filesModified, f)
	}
	sort.Strings(filesModified)

	return len(filesModified) > 0, filesModified, nil
}

// GetDiffStat returns the git diff --stat output comparing the worktree to baseCommit or working copy.
func (m *Manager) GetDiffStat(dir string, baseCommit string) (string, error) {
	if !m.IsGitAvailable() {
		return "", ErrGitNotAvailable
	}
	var cmd *exec.Cmd
	if baseCommit != "" {
		cmd = exec.Command(m.gitPath, "-C", dir, "diff", "--stat", baseCommit)
	} else {
		cmd = exec.Command(m.gitPath, "-C", dir, "diff", "--stat", "HEAD")
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetDiff returns the unified git diff comparing the worktree to baseCommit or working copy.
func (m *Manager) GetDiff(dir string, baseCommit string) (string, error) {
	if !m.IsGitAvailable() {
		return "", ErrGitNotAvailable
	}
	var cmd *exec.Cmd
	if baseCommit != "" {
		cmd = exec.Command(m.gitPath, "-C", dir, "diff", baseCommit)
	} else {
		cmd = exec.Command(m.gitPath, "-C", dir, "diff", "HEAD")
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetBranchDiff returns the unified git diff between HEAD of the primary repository and branch.
func (m *Manager) GetBranchDiff(branch string) (string, error) {
	if !m.IsGitAvailable() {
		return "", ErrGitNotAvailable
	}
	topLevel, err := m.GetTopLevel()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(m.gitPath, "-C", topLevel, "diff", "HEAD..."+branch)
	out, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		cmd2 := exec.Command(m.gitPath, "-C", topLevel, "diff", "HEAD", branch)
		if out2, err2 := cmd2.Output(); err2 == nil {
			return strings.TrimSpace(string(out2)), nil
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// ReconcileResult contains the outcome of a branch reconciliation.
type ReconcileResult struct {
	Strategy    string   `json:"strategy"`
	Commit      string   `json:"commit,omitempty"`
	FilesMerged []string `json:"files_merged,omitempty"`
	Message     string   `json:"message,omitempty"`
}

// ReconcileBranch reconciles an isolated subsession branch into the primary workspace.
// Strategies supported: squash, merge, cherry_pick, discard.
// Validates that the primary working tree is clean before merging or squash merging.
// On discard: removes worktree and deletes the branch.
// On success: executes the git operation, prunes worktree if cleanupWorktree is true,
// and returns the merge commit SHA and files merged.
func (m *Manager) ReconcileBranch(branch string, strategy string, cleanupWorktree bool, sessionID string) (*ReconcileResult, error) {
	if !m.IsGitAvailable() {
		return nil, ErrGitNotAvailable
	}
	if !m.IsGitRepo() {
		return nil, ErrNotGitRepo
	}

	topLevel, err := m.GetTopLevel()
	if err != nil {
		return nil, err
	}

	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "" {
		strategy = "squash"
	}
	switch strategy {
	case "squash", "merge", "cherry_pick", "discard":
	default:
		return nil, fmt.Errorf("unsupported reconciliation strategy %q: must be 'squash', 'merge', 'cherry_pick', or 'discard'", strategy)
	}

	if branch == "" && sessionID != "" {
		branch = fmt.Sprintf("subsession/%s", sessionID)
	}
	if sessionID == "" && branch != "" {
		if strings.HasPrefix(branch, "subsession/") {
			sessionID = strings.TrimPrefix(branch, "subsession/")
		} else if strings.HasPrefix(branch, "please/") {
			sessionID = strings.TrimPrefix(branch, "please/")
		}
	}

	// Strategy: discard
	if strategy == "discard" {
		if sessionID != "" {
			_ = m.RemoveWorktreeBranch(sessionID, branch, true, true)
		} else if branch != "" && m.branchExists(topLevel, branch) {
			_ = exec.Command(m.gitPath, "-C", topLevel, "branch", "-D", branch).Run()
			_ = m.Prune()
		}
		return &ReconcileResult{
			Strategy: "discard",
			Message:  fmt.Sprintf("Branch %s discarded and worktree cleaned up", branch),
		}, nil
	}

	// For merge, squash, cherry_pick: validate that primary workspace is clean
	statusCmd := exec.Command(m.gitPath, "-C", topLevel, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to check primary workspace status: %w", err)
	}
	if len(strings.TrimSpace(string(statusOut))) > 0 {
		return nil, fmt.Errorf("primary workspace has uncommitted changes; working tree must be clean before reconciling branch %s", branch)
	}

	// Validate branch exists
	if !m.branchExists(topLevel, branch) {
		return nil, fmt.Errorf("branch %q does not exist", branch)
	}

	// Auto-commit any pending uncommitted changes in child worktree if active
	if sessionID != "" {
		wtDir, _ := m.GetWorktreeDir(sessionID)
		if wtDir != "" && m.isWorktreeValid(wtDir) {
			cStatus, _ := exec.Command(m.gitPath, "-C", wtDir, "status", "--porcelain").Output()
			if len(strings.TrimSpace(string(cStatus))) > 0 {
				_ = exec.Command(m.gitPath, "-C", wtDir, "add", "-A").Run()
				_ = exec.Command(m.gitPath, "-C", wtDir, "commit", "-m", fmt.Sprintf("Subagent %s automated snapshot before reconcile", sessionID)).Run()
			}
		}
	}

	// Determine modified files prior to merging
	fileMap := make(map[string]bool)
	diffCmd := exec.Command(m.gitPath, "-C", topLevel, "diff", "--name-only", "HEAD..."+branch)
	if dOut, err := diffCmd.Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(dOut)), "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				fileMap[f] = true
			}
		}
	}
	if len(fileMap) == 0 {
		diffCmd2 := exec.Command(m.gitPath, "-C", topLevel, "diff", "--name-only", "HEAD", branch)
		if dOut2, err := diffCmd2.Output(); err == nil {
			for _, f := range strings.Split(strings.TrimSpace(string(dOut2)), "\n") {
				f = strings.TrimSpace(f)
				if f != "" {
					fileMap[f] = true
				}
			}
		}
	}

	// Execute reconciliation strategy
	switch strategy {
	case "merge":
		mergeCmd := exec.Command(m.gitPath, "-C", topLevel, "merge", "--no-ff", "-m", fmt.Sprintf("Merge subagent branch %s", branch), branch)
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			_ = exec.Command(m.gitPath, "-C", topLevel, "merge", "--abort").Run()
			return nil, fmt.Errorf("git merge failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}

	case "squash":
		squashCmd := exec.Command(m.gitPath, "-C", topLevel, "merge", "--squash", branch)
		if out, err := squashCmd.CombinedOutput(); err != nil {
			_ = exec.Command(m.gitPath, "-C", topLevel, "reset", "--hard", "HEAD").Run()
			return nil, fmt.Errorf("git merge --squash failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}
		stCheck, _ := exec.Command(m.gitPath, "-C", topLevel, "status", "--porcelain").Output()
		if len(strings.TrimSpace(string(stCheck))) > 0 {
			commitCmd := exec.Command(m.gitPath, "-C", topLevel, "commit", "-m", fmt.Sprintf("Squash merge subagent branch %s", branch))
			if out, err := commitCmd.CombinedOutput(); err != nil {
				_ = exec.Command(m.gitPath, "-C", topLevel, "reset", "--hard", "HEAD").Run()
				return nil, fmt.Errorf("git commit after squash failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
			}
		}

	case "cherry_pick":
		branchHead, err := exec.Command(m.gitPath, "-C", topLevel, "rev-parse", branch).Output()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve commit for branch %s: %w", branch, err)
		}
		cpCommit := strings.TrimSpace(string(branchHead))
		cpCmd := exec.Command(m.gitPath, "-C", topLevel, "cherry-pick", cpCommit)
		if out, err := cpCmd.CombinedOutput(); err != nil {
			_ = exec.Command(m.gitPath, "-C", topLevel, "cherry-pick", "--abort").Run()
			return nil, fmt.Errorf("git cherry-pick failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}
	}

	headCommit, err := m.GetHeadCommit(topLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to get head commit after reconciliation: %w", err)
	}

	if len(fileMap) == 0 {
		treeCmd := exec.Command(m.gitPath, "-C", topLevel, "diff-tree", "--no-commit-id", "--name-only", "-r", headCommit)
		if tOut, err := treeCmd.Output(); err == nil {
			for _, f := range strings.Split(strings.TrimSpace(string(tOut)), "\n") {
				f = strings.TrimSpace(f)
				if f != "" {
					fileMap[f] = true
				}
			}
		}
	}

	var filesMerged []string
	for f := range fileMap {
		filesMerged = append(filesMerged, f)
	}
	sort.Strings(filesMerged)

	// Clean up worktree and branch if requested
	if cleanupWorktree {
		if sessionID != "" {
			_ = m.RemoveWorktreeBranch(sessionID, branch, true, true)
		} else if branch != "" {
			_ = exec.Command(m.gitPath, "-C", topLevel, "branch", "-D", branch).Run()
			_ = m.Prune()
		}
	}

	return &ReconcileResult{
		Strategy:    strategy,
		Commit:      headCommit,
		FilesMerged: filesMerged,
		Message:     fmt.Sprintf("Successfully reconciled branch %s via %s", branch, strategy),
	}, nil
}


// Prune cleans up stale worktree administrative files from .git.
func (m *Manager) Prune() error {
	if !m.IsGitAvailable() || !m.IsGitRepo() {
		return nil
	}
	topLevel, err := m.GetTopLevel()
	if err != nil {
		return err
	}
	return exec.Command(m.gitPath, "-C", topLevel, "worktree", "prune").Run()
}

// ListWorktrees inspects the repository and returns all active worktrees.
func (m *Manager) ListWorktrees() ([]WorktreeInfo, error) {
	if !m.IsGitAvailable() {
		return nil, ErrGitNotAvailable
	}
	topLevel, err := m.GetTopLevel()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(m.gitPath, "-C", topLevel, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	var results []WorktreeInfo
	lines := strings.Split(string(out), "\n")
	var current WorktreeInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				current.IsPrimary = (current.Path == topLevel)
				current.SessionID = m.deriveSessionID(current.Path, topLevel)
				current.IsActive = m.isWorktreeValid(current.Path)
				results = append(results, current)
				current = WorktreeInfo{}
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") {
			current.Commit = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") {
			branchRef := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(branchRef, "refs/heads/")
		}
	}

	if current.Path != "" {
		current.IsPrimary = (current.Path == topLevel)
		current.SessionID = m.deriveSessionID(current.Path, topLevel)
		current.IsActive = m.isWorktreeValid(current.Path)
		results = append(results, current)
	}

	return results, nil
}

func (m *Manager) isWorktreeValid(dir string) bool {
	if !m.IsGitAvailable() {
		return false
	}
	cmd := exec.Command(m.gitPath, "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func (m *Manager) branchExists(topLevel, branch string) bool {
	cmd := exec.Command(m.gitPath, "-C", topLevel, "rev-parse", "--verify", "refs/heads/"+branch)
	return cmd.Run() == nil
}

func (m *Manager) getCurrentBranch(topLevel string) string {
	cmd := exec.Command(m.gitPath, "-C", topLevel, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}

func (m *Manager) deriveSessionID(worktreePath, topLevel string) string {
	if worktreePath == topLevel {
		return "main"
	}
	return filepath.Base(worktreePath)
}
