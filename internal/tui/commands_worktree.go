package tui

import (
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/worktree"
	tea "github.com/charmbracelet/bubbletea"
)

type WorktreeCommand struct{}

func (c *WorktreeCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	cfgDir, _ := config.GetConfigDir()
	wsDir := m.Config.GetWorkspaceDir()
	wtMgr := worktree.NewManager(cfgDir, wsDir)

	if len(args) == 0 {
		var sb strings.Builder
		sb.WriteString("--- Git Worktree Sandboxing ---\n")
		enabled := m.Config.EnableWorktreeIsolation()
		gitAvail := wtMgr.IsGitAvailable()
		isRepo := wtMgr.IsGitRepo()

		isolationStatus := "disabled (opt-in)"
		if enabled {
			isolationStatus = "enabled"
		}
		sb.WriteString(fmt.Sprintf("  Configuration: %s\n", isolationStatus))
		if !gitAvail {
			sb.WriteString("  Status:        inactive (git binary not found in PATH)\n")
		} else if !isRepo {
			sb.WriteString("  Status:        inactive (workspace is not a git repository)\n")
		} else {
			sb.WriteString("  Status:        active\n")
			topLevel, _ := wtMgr.GetTopLevel()
			sb.WriteString(fmt.Sprintf("  Primary Repo:  %s\n", topLevel))
			activeDir, _ := wtMgr.GetWorktreeDir(m.SessionID)
			sb.WriteString(fmt.Sprintf("  Session:       %s\n", m.SessionID))
			sb.WriteString(fmt.Sprintf("  Active Dir:    %s\n", activeDir))
			if m.SessionID != "" && m.SessionID != "main" {
				sb.WriteString(fmt.Sprintf("  Branch:        please/%s\n", m.SessionID))
			} else {
				sb.WriteString("  Branch:        (primary checkout)\n")
			}
		}
		sb.WriteString("\nCommands:\n  /worktree list           List all worktrees\n  /worktree remove <name>  Remove worktree checkout\n  /config worktree on|off  Toggle isolation\n")
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "worktree", "Worktree Status", sb.String(), ""))
		return m, nil
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		if !wtMgr.IsGitAvailable() {
			m.Notification = "Git binary not found in PATH"
			return m, nil
		}
		if !wtMgr.IsGitRepo() {
			m.Notification = "Workspace is not inside a git repository"
			return m, nil
		}
		list, err := wtMgr.ListWorktrees()
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to list worktrees: %v", err)
			return m, nil
		}
		var sb strings.Builder
		sb.WriteString("--- Active Git Worktrees ---\n")
		for _, wt := range list {
			marker := "  "
			if wt.SessionID == m.SessionID {
				marker = "➜ "
			}
			primaryTag := ""
			if wt.IsPrimary {
				primaryTag = " [primary]"
			}
			sb.WriteString(fmt.Sprintf("%s%-16s %-24s %s%s\n", marker, wt.SessionID, wt.Branch, wt.Path, primaryTag))
		}
		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "worktree", "Active Git Worktrees", sb.String(), ""))
		return m, nil

	case "remove", "rm", "delete":
		if len(args) < 2 {
			m.Notification = "Usage: /worktree remove <session-name>"
			return m, nil
		}
		target := args[1]
		if target == "main" {
			m.Notification = "Cannot remove primary repository worktree for 'main'"
			return m, nil
		}
		if err := wtMgr.RemoveWorktree(target, true, true); err != nil {
			m.Notification = fmt.Sprintf("Failed to remove worktree: %v", err)
			return m, nil
		}
		m.Notification = fmt.Sprintf("Worktree for session %q removed successfully", target)
		return m, nil

	default:
		m.Notification = "Usage: /worktree [list|remove <session>]"
		return m, nil
	}
}
