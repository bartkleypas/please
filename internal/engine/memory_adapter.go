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

func toToolMemoryItem(stMem *storage.Memory) *tools.MemoryItem {
	if stMem == nil {
		return nil
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
	}
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
	return toToolMemoryItem(stMem), nil
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
		if item := toToolMemoryItem(&m); item != nil {
			out = append(out, *item)
		}
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
		if item := toToolMemoryItem(&m); item != nil {
			diag.MostAccessed = append(diag.MostAccessed, *item)
		}
	}
	for _, m := range stDiag.StaleCandidates {
		if item := toToolMemoryItem(&m); item != nil {
			diag.StaleCandidates = append(diag.StaleCandidates, *item)
		}
	}
	return diag, nil
}
