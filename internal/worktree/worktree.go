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
