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
	var obsStore tools.ObservationStore
	if s, ok := m.Storage.(tools.ObservationStore); ok {
		obsStore = s
	}
	m.Registry.RegisterObservationStore(obsStore, func(receiptID string) (string, error) {
		return m.lookupLegacyObservation(receiptID)
	})
	m.RegisterDelegation(NewSubagentOrchestrator(m, nil, nil))
}

// RegisterDelegation registers the spawn_subagent tool bound to runner into the manager's registry (ADR 022).
func (m *Manager) RegisterDelegation(runner tools.SubagentRunner) {
	if m.Registry != nil && runner != nil {
		m.Registry.RegisterDelegation(runner)
	}
}

// RegisterReconciliation registers the reconcile_subagent tool into the manager's registry.
func (m *Manager) RegisterReconciliation(reconciler tools.SubagentReconciler) {
	if m.Registry != nil && reconciler != nil {
		m.Registry.RegisterReconciliation(reconciler)
	}
}

// RegisterAuditor registers the inspect_subagent tool into the manager's registry.
func (m *Manager) RegisterAuditor(auditor tools.SubagentAuditor) {
	if m.Registry != nil && auditor != nil {
		m.Registry.RegisterAuditor(auditor)
	}
}
