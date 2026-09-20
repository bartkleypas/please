package storage

import (
	"time"

	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
)

// Storage defines the interface for persisting the conversation graph
type Storage interface {
	SaveNode(node *graph.Node) error
	LoadGraph() (*graph.Graph, string, error)
	GarbageCollect() (int64, error)
	UpdateNodeMetadata(node *graph.Node) error
	UpdateNodeParentID(nodeID, newParentID string) error
	UpdateNodeObservations(nodeID string, obs []providers.ToolObservation) error
	SaveSessionHead(sessionID, nodeID string) error
	GetSessionHead(sessionID string) (string, error)
	ListSessions() (map[string]string, error)
}

// MemoryScope defines the visibility and boundary of a memory record.
type MemoryScope string

const (
	ScopeWorkspace MemoryScope = "workspace" // Tied to the current Git repository or workspace directory
	ScopeGlobal    MemoryScope = "global"    // Universal user preferences, shared across all projects
	ScopeSession   MemoryScope = "session"   // Ephemeral to the active conversation session ID
)

// MemoryCategory establishes semantic grouping for selective filtering.
type MemoryCategory string

const (
	CategoryPreference   MemoryCategory = "preference"   // User habits, formatting choices, communication tone
	CategoryFact         MemoryCategory = "fact"         // Discovered environment truths (e.g. Go version, OS quirks)
	CategoryArchitecture MemoryCategory = "architecture" // Design decisions, invariants, module boundaries
	CategoryConstraint   MemoryCategory = "constraint"   // Strict guidelines (e.g. "Never use CGo", "Preserve WAL")
	CategoryWorkflow     MemoryCategory = "workflow"     // Common task recipes (e.g. "How to run hermetic tests")
	CategoryScratchpad   MemoryCategory = "scratchpad"   // Long-lived working notes
)

// Memory represents an atomic, persistent unit of agent knowledge.
type Memory struct {
	ID             string         `json:"id"`
	Key            string         `json:"key"`
	Content        string         `json:"content"`
	Category       MemoryCategory `json:"category"`
	Tags           []string       `json:"tags,omitempty"`
	Scope          MemoryScope    `json:"scope"`
	Confidence     float64        `json:"confidence"`
	SessionID      string         `json:"session_id,omitempty"`
	SourceNodeID   string         `json:"source_node_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	AccessCount    int            `json:"access_count"`
	LastAccessedAt *time.Time     `json:"last_accessed_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// MemoryFilter encapsulates query parameters for memory recall and diagnostics.
type MemoryFilter struct {
	Query     string         `json:"query,omitempty"`
	Key       string         `json:"key,omitempty"`
	Category  MemoryCategory `json:"category,omitempty"`
	Scope     MemoryScope    `json:"scope,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Limit     int            `json:"limit,omitempty"`
	Touch     bool           `json:"touch,omitempty"` // If true, increments access_count and updates last_accessed_at (agent recall)
}

// MemoryDiagnostics captures aggregate health and volume telemetry of the memory store.
type MemoryDiagnostics struct {
	TotalMemories  int                       `json:"total_memories"`
	ByScope        map[MemoryScope]int       `json:"by_scope"`
	ByCategory     map[MemoryCategory]int    `json:"by_category"`
	StorageBytes   int64                     `json:"storage_bytes"`
	MostAccessed   []Memory                  `json:"most_accessed"`
	StaleCandidates []Memory                 `json:"stale_candidates"`
}

// MemoryStore defines the interface for persisting and recalling atomic agent memories.
type MemoryStore interface {
	SaveMemory(mem *Memory) error
	GetMemory(scope MemoryScope, sessionID, key string) (*Memory, error)
	QueryMemories(filter MemoryFilter) ([]Memory, error)
	DeleteMemory(scope MemoryScope, sessionID, key string) error
	DiagnoseMemories(scope MemoryScope, sessionID string) (*MemoryDiagnostics, error)
}

var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

// parseFlexibleTimestamp converts various timestamp formats or representations into time.Time.
func parseFlexibleTimestamp(raw interface{}) time.Time {
	switch ts := raw.(type) {
	case time.Time:
		return ts
	case int64:
		return time.Unix(ts, 0)
	case []byte:
		raw = string(ts)
	}

	if str, ok := raw.(string); ok {
		for _, layout := range timeFormats {
			if t, err := time.Parse(layout, str); err == nil {
				return t
			}
		}
	}
	return time.Now()
}
