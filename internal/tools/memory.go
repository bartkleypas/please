package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/domain"
)

type memoryContextKey string

const (
	ContextKeySessionID memoryContextKey = "please_session_id"
	ContextKeyNodeID    memoryContextKey = "please_node_id"
)

// WithMemoryContext attaches active session and DAG node IDs to the context for lineage attribution.
func WithMemoryContext(ctx context.Context, sessionID, nodeID string) context.Context {
	ctx = context.WithValue(ctx, ContextKeySessionID, sessionID)
	return context.WithValue(ctx, ContextKeyNodeID, nodeID)
}

// MemoryContextFromContext extracts session and DAG node IDs from the context.
func MemoryContextFromContext(ctx context.Context) (sessionID, nodeID string) {
	if v := ctx.Value(ContextKeySessionID); v != nil {
		sessionID, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyNodeID); v != nil {
		nodeID, _ = v.(string)
	}
	return sessionID, nodeID
}

// MemoryItem represents an atomic unit of knowledge in the tools domain.
type MemoryItem struct {
	ID             string         `json:"id"`
	Key            string         `json:"key"`
	Content        string         `json:"content"`
	Category       string         `json:"category"`
	Tags           []string       `json:"tags,omitempty"`
	Scope          string         `json:"scope"`
	Confidence     float64        `json:"confidence"`
	SessionID      string         `json:"session_id,omitempty"`
	SourceNodeID   string         `json:"source_node_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	AccessCount    int            `json:"access_count"`
	LastAccessedAt *time.Time     `json:"last_accessed_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// MemoryFilter encapsulates query parameters for memory recall.
type MemoryFilter struct {
	Query     string   `json:"query,omitempty"`
	Key       string   `json:"key,omitempty"`
	Category  string   `json:"category,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Touch     bool     `json:"touch,omitempty"` // If true, increments access_count and updates last_accessed_at (agent recall)
}

// MemoryDiagnostics captures aggregate health and volume telemetry of the memory store.
type MemoryDiagnostics struct {
	TotalMemories   int            `json:"total_memories"`
	ByScope         map[string]int `json:"by_scope"`
	ByCategory      map[string]int `json:"by_category"`
	StorageBytes    int64          `json:"storage_bytes"`
	MostAccessed    []MemoryItem   `json:"most_accessed"`
	StaleCandidates []MemoryItem   `json:"stale_candidates"`
}

// MemoryStore defines the interface required by cybernetic memory_* tools.
type MemoryStore interface {
	SaveMemory(mem *MemoryItem) error
	GetMemory(scope, sessionID, key string) (*MemoryItem, error)
	QueryMemories(filter MemoryFilter) ([]MemoryItem, error)
	DeleteMemory(scope, sessionID, key string) error
	DiagnoseMemories(scope, sessionID string) (*MemoryDiagnostics, error)
}

var quarantinedSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bghp_[a-zA-Z0-9]{36,}\b`),
	regexp.MustCompile(`(?i)\bsk-[a-zA-Z0-9_-]{20,}\b`),
	regexp.MustCompile(`(?i)\bAIza[a-zA-Z0-9_\-]{35}\b`),
	regexp.MustCompile(`-----BEGIN (?:RSA|EC|DSA|OPENSSH|PRIVATE) KEY-----`),
}

// isSecretQuarantined checks whether memory content contains raw secret tokens or private keys (ADR 011).
func isSecretQuarantined(content string) bool {
	for _, re := range quarantinedSecretPatterns {
		if re.MatchString(content) {
			return true
		}
	}
	return false
}

// MemoryTools returns the full cybernetic suite of memory tools bound to the provided MemoryStore.
func MemoryTools(store MemoryStore, defaultScope ...string) []Tool {
	defScope := "workspace"
	if len(defaultScope) > 0 && defaultScope[0] != "" {
		defScope = defaultScope[0]
	}

	return []Tool{
		MemoryRecallTool(store, defScope),
		MemoryDiagnoseTool(store),
		MemoryStoreTool(store, defScope),
		MemoryDeleteTool(store, defScope),
	}
}

// MemoryStoreTool returns the CategoryMutate tool for persisting atomic knowledge.
func MemoryStoreTool(store MemoryStore, defaultScope string) Tool {
	return Tool{
		Name:     "memory_store",
		Category: domain.CategoryMutate,
		Description: "Stores or updates an atomic, persistent unit of knowledge (constraint, architectural decision, environment fact, or workflow recipe) in the project memory vault. " +
			"Overwrites existing entries with matching (scope, key). Do not store conversation transcripts; the DAG already preserves turn history.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Unique, descriptive identifier for the memory (e.g. 'arch:storage:wal', 'constraint:no-cgo', 'pref:diffs').",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The distilled knowledge, invariant, rule, or preference to remember in concise markdown.",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"preference", "fact", "architecture", "constraint", "workflow", "scratchpad"},
					"description": "Semantic classification of the memory.",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"workspace", "global", "session"},
					"default":     defaultScope,
					"description": "Visibility scope. Defaults to 'workspace' (shared across repo sessions). Use 'session' for private branch scratchpads.",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional tags for filtering and indexing.",
				},
				"confidence": map[string]interface{}{
					"type":        "number",
					"minimum":     0.0,
					"maximum":     1.0,
					"default":     1.0,
					"description": "Confidence score between 0.0 and 1.0.",
				},
			},
			"required": []string{"key", "content", "category"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if store == nil {
				return "", fmt.Errorf("memory store is not available in this environment")
			}

			key, _ := args["key"].(string)
			content, _ := args["content"].(string)
			catStr, _ := args["category"].(string)
			scopeStr, _ := args["scope"].(string)

			if strings.TrimSpace(key) == "" {
				return "", fmt.Errorf("memory key cannot be empty")
			}
			if strings.TrimSpace(content) == "" {
				return "", fmt.Errorf("memory content cannot be empty")
			}
			if strings.TrimSpace(catStr) == "" {
				return "", fmt.Errorf("memory category is required")
			}

			// Credential quarantine check (ADR 011)
			if isSecretQuarantined(content) || isSecretQuarantined(key) {
				return "", fmt.Errorf("security violation (ADR 011): memory contains quarantined credentials or private keys")
			}

			scope := scopeStr
			if scope == "" {
				scope = defaultScope
			}
			if scope == "" {
				scope = "workspace"
			}

			sessID, nodeID := MemoryContextFromContext(ctx)

			confidence := 1.0
			if cf, ok := args["confidence"].(float64); ok && cf > 0 {
				confidence = cf
			}

			var tags []string
			if rawTags, ok := args["tags"].([]interface{}); ok {
				for _, t := range rawTags {
					if s, ok := t.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
			}

			mem := &MemoryItem{
				Key:          key,
				Content:      content,
				Category:     catStr,
				Scope:        scope,
				Tags:         tags,
				Confidence:   confidence,
				SessionID:    sessID,
				SourceNodeID: nodeID,
			}

			if err := store.SaveMemory(mem); err != nil {
				return "", fmt.Errorf("failed to store memory: %w", err)
			}

			return fmt.Sprintf("Stored memory %q [category: %s, scope: %s] successfully.", key, catStr, scope), nil
		},
	}
}

// MemoryRecallTool returns the CategorySensory tool for querying memories.
func MemoryRecallTool(store MemoryStore, defaultScope string) Tool {
	return Tool{
		Name:     "memory_recall",
		Category: domain.CategorySensory,
		Description: "Queries the project memory vault for stored guidelines, constraints, architectural facts, or workflows. " +
			"Supports full-text search, exact key matches, category filters, and tags.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Full-text search keywords to match against memory content, keys, or tags.",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Exact key or prefix to query.",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"preference", "fact", "architecture", "constraint", "workflow", "scratchpad"},
					"description": "Optional category filter.",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"workspace", "global", "session", "all"},
					"default":     defaultScope,
					"description": "Scope filter. Defaults to 'workspace'.",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Filter by tags.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"default":     10,
					"description": "Maximum number of memories to return.",
				},
			},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if store == nil {
				return "", fmt.Errorf("memory store is not available in this environment")
			}

			filter := MemoryFilter{
				Touch: true, // Agent recalls actively warm up access telemetry
			}
			if q, ok := args["query"].(string); ok {
				filter.Query = strings.TrimSpace(q)
			}
			if k, ok := args["key"].(string); ok {
				filter.Key = strings.TrimSpace(k)
			}
			if cat, ok := args["category"].(string); ok {
				filter.Category = cat
			}
			if sc, ok := args["scope"].(string); ok {
				filter.Scope = sc
			} else if defaultScope != "" {
				filter.Scope = defaultScope
			}
			if l, ok := args["limit"].(float64); ok && l > 0 {
				filter.Limit = int(l)
			}

			if rawTags, ok := args["tags"].([]interface{}); ok {
				for _, t := range rawTags {
					if s, ok := t.(string); ok && s != "" {
						filter.Tags = append(filter.Tags, s)
					}
				}
			}

			sessID, _ := MemoryContextFromContext(ctx)
			filter.SessionID = sessID

			memories, err := store.QueryMemories(filter)
			if err != nil {
				return "", fmt.Errorf("failed to recall memories: %w", err)
			}

			if len(memories) == 0 {
				target := "all active scopes"
				if filter.Query != "" {
					target = fmt.Sprintf("query %q", filter.Query)
				} else if filter.Key != "" {
					target = fmt.Sprintf("key %q", filter.Key)
				}
				return fmt.Sprintf("No memories found matching %s.", target), nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Recalled %d memories:\n", len(memories)))
			for _, m := range memories {
				tagStr := ""
				if len(m.Tags) > 0 {
					tagStr = fmt.Sprintf(" (tags: %s)", strings.Join(m.Tags, ", "))
				}
				sb.WriteString(fmt.Sprintf("\n• [%s | %s] %s%s\n  %s\n", m.Scope, m.Category, m.Key, tagStr, strings.TrimSpace(m.Content)))
			}

			return sb.String(), nil
		},
	}
}

// MemoryDeleteTool returns the CategoryMutate tool for deleting or forgetting a memory.
func MemoryDeleteTool(store MemoryStore, defaultScope string) Tool {
	return Tool{
		Name:        "memory_delete",
		Category:    domain.CategoryMutate,
		Description: "Deletes or forgets an obsolete, contradicted, or superseded memory by key and scope.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "The exact key of the memory to remove.",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"workspace", "global", "session"},
					"default":     defaultScope,
					"description": "Scope of the memory to delete.",
				},
			},
			"required": []string{"key"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if store == nil {
				return "", fmt.Errorf("memory store is not available in this environment")
			}

			key, _ := args["key"].(string)
			scopeStr, _ := args["scope"].(string)
			if strings.TrimSpace(key) == "" {
				return "", fmt.Errorf("memory key is required")
			}

			scope := scopeStr
			if scope == "" {
				scope = defaultScope
			}
			if scope == "" {
				scope = "workspace"
			}

			sessID, _ := MemoryContextFromContext(ctx)

			if err := store.DeleteMemory(scope, sessID, key); err != nil {
				return "", fmt.Errorf("failed to delete memory: %w", err)
			}

			return fmt.Sprintf("Deleted memory %q [scope: %s] successfully.", key, scope), nil
		},
	}
}

// MemoryDiagnoseTool returns the CategorySensory tool for inspecting memory bank telemetry.
func MemoryDiagnoseTool(store MemoryStore) Tool {
	return Tool{
		Name:        "memory_diagnose",
		Category:    domain.CategorySensory,
		Description: "Provides comprehensive diagnostic telemetry over the project memory vault, including volume, category distributions, storage footprints, and stale memories.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"workspace", "global", "session", "all"},
					"default":     "all",
					"description": "Scope to diagnose.",
				},
				"detailed": map[string]interface{}{
					"type":        "boolean",
					"default":     false,
					"description": "Whether to return full JSON telemetry.",
				},
			},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if store == nil {
				return "", fmt.Errorf("memory store is not available in this environment")
			}

			scopeStr, _ := args["scope"].(string)
			if scopeStr == "" {
				scopeStr = "all"
			}
			detailed, _ := args["detailed"].(bool)

			sessID, _ := MemoryContextFromContext(ctx)
			diag, err := store.DiagnoseMemories(scopeStr, sessID)
			if err != nil {
				return "", fmt.Errorf("failed to diagnose memories: %w", err)
			}

			if detailed {
				raw, _ := json.MarshalIndent(diag, "", "  ")
				return string(raw), nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Memory Vault Diagnostics (scope: %s):\n", scopeStr))
			sb.WriteString(fmt.Sprintf("• Total Memories: %d (~%d bytes)\n", diag.TotalMemories, diag.StorageBytes))

			if len(diag.ByScope) > 0 {
				sb.WriteString("• By Scope:\n")
				for s, count := range diag.ByScope {
					sb.WriteString(fmt.Sprintf("  - %s: %d\n", s, count))
				}
			}

			if len(diag.ByCategory) > 0 {
				sb.WriteString("• By Category:\n")
				for c, count := range diag.ByCategory {
					sb.WriteString(fmt.Sprintf("  - %s: %d\n", c, count))
				}
			}

			if len(diag.MostAccessed) > 0 {
				sb.WriteString("• Top Accessed Invariants:\n")
				for _, m := range diag.MostAccessed {
					sb.WriteString(fmt.Sprintf("  - %s (hits: %d)\n", m.Key, m.AccessCount))
				}
			}

			if len(diag.StaleCandidates) > 0 {
				sb.WriteString(fmt.Sprintf("• Stale/Low-Access Candidates: %d item(s)\n", len(diag.StaleCandidates)))
			}

			return sb.String(), nil
		},
	}
}
