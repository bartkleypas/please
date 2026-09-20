package engine

import (
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
)

type memoryStoreAdapter struct {
	store storage.MemoryStore
}

// NewMemoryToolsAdapter wraps a storage.MemoryStore into tools.MemoryStore.
func NewMemoryToolsAdapter(store storage.MemoryStore) tools.MemoryStore {
	if store == nil {
		return nil
	}
	return &memoryStoreAdapter{store: store}
}

func (a *memoryStoreAdapter) SaveMemory(mem *tools.MemoryItem) error {
	if mem == nil {
		return nil
	}
	stMem := &storage.Memory{
		ID:             mem.ID,
		Key:            mem.Key,
		Content:        mem.Content,
		Category:       storage.MemoryCategory(mem.Category),
		Tags:           mem.Tags,
		Scope:          storage.MemoryScope(mem.Scope),
		Confidence:     mem.Confidence,
		SessionID:      mem.SessionID,
		SourceNodeID:   mem.SourceNodeID,
		Metadata:       mem.Metadata,
		AccessCount:    mem.AccessCount,
		LastAccessedAt: mem.LastAccessedAt,
		CreatedAt:      mem.CreatedAt,
		UpdatedAt:      mem.UpdatedAt,
	}
	return a.store.SaveMemory(stMem)
}

func (a *memoryStoreAdapter) GetMemory(scope, sessionID, key string) (*tools.MemoryItem, error) {
	stMem, err := a.store.GetMemory(storage.MemoryScope(scope), sessionID, key)
	if err != nil || stMem == nil {
		return nil, err
	}
	return &tools.MemoryItem{
		ID:             stMem.ID,
		Key:            stMem.Key,
		Content:        stMem.Content,
		Category:       string(stMem.Category),
		Tags:           stMem.Tags,
		Scope:          string(stMem.Scope),
		Confidence:     stMem.Confidence,
		SessionID:      stMem.SessionID,
		SourceNodeID:   stMem.SourceNodeID,
		Metadata:       stMem.Metadata,
		AccessCount:    stMem.AccessCount,
		LastAccessedAt: stMem.LastAccessedAt,
		CreatedAt:      stMem.CreatedAt,
		UpdatedAt:      stMem.UpdatedAt,
	}, nil
}

func (a *memoryStoreAdapter) QueryMemories(filter tools.MemoryFilter) ([]tools.MemoryItem, error) {
	stFilter := storage.MemoryFilter{
		Query:     filter.Query,
		Key:       filter.Key,
		Category:  storage.MemoryCategory(filter.Category),
		Scope:     storage.MemoryScope(filter.Scope),
		SessionID: filter.SessionID,
		Tags:      filter.Tags,
		Limit:     filter.Limit,
		Touch:     filter.Touch,
	}
	stMems, err := a.store.QueryMemories(stFilter)
	if err != nil {
		return nil, err
	}
	var out []tools.MemoryItem
	for _, m := range stMems {
		out = append(out, tools.MemoryItem{
			ID:             m.ID,
			Key:            m.Key,
			Content:        m.Content,
			Category:       string(m.Category),
			Tags:           m.Tags,
			Scope:          string(m.Scope),
			Confidence:     m.Confidence,
			SessionID:      m.SessionID,
			SourceNodeID:   m.SourceNodeID,
			Metadata:       m.Metadata,
			AccessCount:    m.AccessCount,
			LastAccessedAt: m.LastAccessedAt,
			CreatedAt:      m.CreatedAt,
			UpdatedAt:      m.UpdatedAt,
		})
	}
	return out, nil
}

func (a *memoryStoreAdapter) DeleteMemory(scope, sessionID, key string) error {
	return a.store.DeleteMemory(storage.MemoryScope(scope), sessionID, key)
}

func (a *memoryStoreAdapter) DiagnoseMemories(scope, sessionID string) (*tools.MemoryDiagnostics, error) {
	stDiag, err := a.store.DiagnoseMemories(storage.MemoryScope(scope), sessionID)
	if err != nil {
		return nil, err
	}
	diag := &tools.MemoryDiagnostics{
		TotalMemories: stDiag.TotalMemories,
		ByScope:       make(map[string]int),
		ByCategory:    make(map[string]int),
		StorageBytes:  stDiag.StorageBytes,
	}
	for s, c := range stDiag.ByScope {
		diag.ByScope[string(s)] = c
	}
	for cat, c := range stDiag.ByCategory {
		diag.ByCategory[string(cat)] = c
	}
	for _, m := range stDiag.MostAccessed {
		diag.MostAccessed = append(diag.MostAccessed, tools.MemoryItem{
			ID:          m.ID,
			Key:         m.Key,
			Category:    string(m.Category),
			Scope:       string(m.Scope),
			AccessCount: m.AccessCount,
		})
	}
	for _, m := range stDiag.StaleCandidates {
		diag.StaleCandidates = append(diag.StaleCandidates, tools.MemoryItem{
			ID:          m.ID,
			Key:         m.Key,
			Category:    string(m.Category),
			Scope:       string(m.Scope),
			AccessCount: m.AccessCount,
			UpdatedAt:   m.UpdatedAt,
		})
	}
	return diag, nil
}
