package engine

import (
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
)

// RegisterDefaultTools adds the default toolset to the manager's registry scoped to workspaceDir.
func (m *Manager) RegisterDefaultTools(workspaceDir ...string) {
	ws := m.WorkspaceDir
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
		m.WorkspaceDir = ws
	}
	tools.RegisterDefaultTools(m.Registry, workspaceDir...)
	if memStore, ok := m.Storage.(storage.MemoryStore); ok && memStore != nil {
		m.Registry.RegisterMemory(NewMemoryToolsAdapter(memStore), "workspace")
	}
}
