package engine

import (
	"github.com/bartkleypas/please/internal/tools"
)

// GetDefaultTools returns a list of basic tools for the application scoped to a workspace directory.
func GetDefaultTools(workspaceDir ...string) []Tool {
	return tools.GetDefaultTools(workspaceDir...)
}

// RegisterDefaultTools adds the default toolset to the manager's registry scoped to workspaceDir.
func (m *Manager) RegisterDefaultTools(workspaceDir ...string) {
	ws := m.WorkspaceDir
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
		m.WorkspaceDir = ws
	}
	tools.RegisterDefaultTools(m.Registry, ws)
}
