package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/google/uuid"

	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
)

// SQLiteStorage implements Storage using an SQLite database in WAL mode
type SQLiteStorage struct {
	DBPath        string
	encryptionKey string
	db            *sql.DB
}

// NewSQLiteStorage creates a new instance of SQLiteStorage and initializes the schema
func NewSQLiteStorage(path, key string) (*SQLiteStorage, error) {
	resolvedPath := os.ExpandEnv(path)
	if strings.HasPrefix(resolvedPath, "~/") || resolvedPath == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if resolvedPath == "~" {
				resolvedPath = home
			} else {
				resolvedPath = filepath.Join(home, resolvedPath[2:])
			}
		}
	}
	s := &SQLiteStorage{DBPath: filepath.Clean(resolvedPath), encryptionKey: key}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	s.db = db

	// Initialize schema
	query := `
	CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY,
		parent_id TEXT,
		role TEXT,
		content TEXT,
		thought TEXT,
		timestamp DATETIME,
		tool_calls TEXT,
		tool_call_id TEXT,
		observations TEXT,
		metadata TEXT,
		deleted BOOLEAN DEFAULT 0,
		internal BOOLEAN DEFAULT 0,
		images TEXT
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		head_node_id TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		key TEXT NOT NULL,
		content TEXT NOT NULL,
		category TEXT NOT NULL,
		tags TEXT,
		scope TEXT NOT NULL DEFAULT 'workspace',
		confidence REAL NOT NULL DEFAULT 1.0,
		session_id TEXT,
		source_node_id TEXT,
		metadata TEXT,
		access_count INTEGER NOT NULL DEFAULT 0,
		last_accessed_at DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_scope_key 
	ON memories(scope, COALESCE(session_id, ''), key);

	CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
	CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
	CREATE INDEX IF NOT EXISTS idx_memories_updated_at ON memories(updated_at);
	`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("failed to initialize sqlite schema: %w", err)
	}

	// Initialize FTS5 for memories if supported
	initMemoriesFTS(db)

	// Migrations: Add missing columns if they don't exist
	if err := s.migrate(db); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return s, nil
}

func (s *SQLiteStorage) migrate(db *sql.DB) error {
	columns := make(map[string]bool)
	rows, err := db.Query("PRAGMA table_info(nodes)")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var name, dtype string
		var cid, notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &dtype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading table info: %w", err)
	}

	migrations := []struct {
		column string
		query  string
	}{
		{"deleted", "ALTER TABLE nodes ADD COLUMN deleted BOOLEAN DEFAULT 0"},
		{"thought", "ALTER TABLE nodes ADD COLUMN thought TEXT"},
		{"internal", "ALTER TABLE nodes ADD COLUMN internal BOOLEAN DEFAULT 0"},
		{"observations", "ALTER TABLE nodes ADD COLUMN observations TEXT"},
		{"images", "ALTER TABLE nodes ADD COLUMN images TEXT"},
	}

	for _, m := range migrations {
		if !columns[m.column] {
			if _, err := db.Exec(m.query); err != nil {
				return fmt.Errorf("failed to add column %s: %w", m.column, err)
			}
		}
	}

	return nil
}

func (s *SQLiteStorage) open() (*sql.DB, error) {
	db, err := sql.Open("sqlite", s.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// For SQLite, we want a single connection for writing to avoid BUSY errors,
	// especially in WAL mode where readers are non-blocking.
	db.SetMaxOpenConns(1)

	// Try to set pragmas with aggressive retries to handle concurrent initialization
	var lastErr error
	for i := 0; i < 30; i++ {
		_, err := db.Exec("PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL;")
		if err == nil {
			return db, nil
		}
		lastErr = err
		// Exponential backoff with some jitter
		time.Sleep(time.Duration(50+i*20) * time.Millisecond)
	}

	db.Close()
	return nil, fmt.Errorf("failed to initialize sqlite pragmas: %w", lastErr)
}

// SaveNode inserts a single node into the SQLite database
func (s *SQLiteStorage) SaveNode(node *graph.Node) error {
	toolCallsJSON, err := json.Marshal(node.ToolCalls)
	if err != nil {
		return fmt.Errorf("failed to marshal tool calls: %w", err)
	}

	obsJSON, err := json.Marshal(node.Observations)
	if err != nil {
		return fmt.Errorf("failed to marshal observations: %w", err)
	}

	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	imagesJSON, err := json.Marshal(node.Images)
	if err != nil {
		return fmt.Errorf("failed to marshal images: %w", err)
	}

	encContent, err := EncryptField(node.Content, s.encryptionKey)
	if err != nil {
		return err
	}
	encThought, err := EncryptField(node.Thought, s.encryptionKey)
	if err != nil {
		return err
	}
	encToolCalls, err := EncryptField(string(toolCallsJSON), s.encryptionKey)
	if err != nil {
		return err
	}
	encObservations, err := EncryptField(string(obsJSON), s.encryptionKey)
	if err != nil {
		return err
	}
	encImages, err := EncryptField(string(imagesJSON), s.encryptionKey)
	if err != nil {
		return err
	}

	if s.encryptionKey != "" {
		node.Encrypted = true
	}

	query := `
	INSERT INTO nodes (id, parent_id, role, content, thought, timestamp, tool_calls, tool_call_id, observations, metadata, deleted, internal, images)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		parent_id = excluded.parent_id,
		role = excluded.role,
		content = excluded.content,
		thought = excluded.thought,
		timestamp = excluded.timestamp,
		tool_calls = excluded.tool_calls,
		tool_call_id = excluded.tool_call_id,
		observations = CASE 
			WHEN excluded.observations IS NOT NULL AND excluded.observations != 'null' AND excluded.observations != '[]' 
			THEN excluded.observations 
			ELSE nodes.observations 
		END,
		metadata = excluded.metadata,
		deleted = excluded.deleted,
		internal = excluded.internal,
		images = excluded.images
	`

	tsVal := node.Timestamp
	if tsVal.IsZero() {
		tsVal = time.Now()
	}

	_, err = s.db.Exec(query,
		node.ID,
		node.ParentID,
		string(node.Role),
		encContent,
		encThought,
		tsVal.Format(time.RFC3339Nano),
		encToolCalls,
		node.ToolCallID,
		encObservations,
		string(metadataJSON),
		node.Deleted,
		node.Internal,
		encImages,
	)
	if err != nil {
		return fmt.Errorf("failed to insert node into sqlite: %w", err)
	}

	return nil
}

// UpdateNodeMetadata updates the metadata and deletion state of an existing node
func (s *SQLiteStorage) UpdateNodeMetadata(node *graph.Node) error {
	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `UPDATE nodes SET metadata = ?, deleted = ? WHERE id = ?`
	_, err = s.db.Exec(query, string(metadataJSON), node.Deleted, node.ID)
	if err != nil {
		return fmt.Errorf("failed to update node in sqlite: %w", err)
	}
	return nil
}

// GarbageCollect permanently removes deleted nodes and vacuums the database
func (s *SQLiteStorage) GarbageCollect() (int64, error) {
	query := `DELETE FROM nodes WHERE deleted = 1`
	res, err := s.db.Exec(query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete nodes: %w", err)
	}

	count, _ := res.RowsAffected()

	if _, err := s.db.Exec("VACUUM;"); err != nil {
		return count, fmt.Errorf("failed to vacuum sqlite: %w", err)
	}

	return count, nil
}

// UpdateNodeParentID updates the parent ID of a node in the database
func (s *SQLiteStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	query := `UPDATE nodes SET parent_id = ? WHERE id = ?`
	_, err := s.db.Exec(query, newParentID, nodeID)
	if err != nil {
		return fmt.Errorf("failed to update parent_id in sqlite: %w", err)
	}
	return nil
}

// UpdateNodeObservations updates the observations of an existing node in the database
func (s *SQLiteStorage) UpdateNodeObservations(nodeID string, obs []providers.ToolObservation) error {
	obsJSON, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("failed to marshal observations: %w", err)
	}

	encObs, err := EncryptField(string(obsJSON), s.encryptionKey)
	if err != nil {
		return err
	}

	query := `UPDATE nodes SET observations = ? WHERE id = ?`
	_, err = s.db.Exec(query, encObs, nodeID)
	if err != nil {
		return fmt.Errorf("failed to update observations in sqlite: %w", err)
	}
	return nil
}

// LoadGraph reads all nodes from the SQLite database and reconstructs the Graph.
func (s *SQLiteStorage) LoadGraph() (*graph.Graph, string, error) {
	query := `SELECT id, parent_id, role, content, thought, timestamp, tool_calls, tool_call_id, observations, metadata, deleted, internal, images FROM nodes ORDER BY timestamp ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query nodes from sqlite: %w", err)
	}
	defer rows.Close()

	g := graph.NewGraph()
	var lastID string

	for rows.Next() {
		var node graph.Node
		var roleStr, toolCallsJSON, obsJSON, metadataJSON string
		var imagesJSON sql.NullString
		var thought sql.NullString
		var deleted bool
		var rawTimestamp interface{}

		err := rows.Scan(
			&node.ID,
			&node.ParentID,
			&roleStr,
			&node.Content,
			&thought,
			&rawTimestamp,
			&toolCallsJSON,
			&node.ToolCallID,
			&obsJSON,
			&metadataJSON,
			&deleted,
			&node.Internal,
			&imagesJSON,
		)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan node from sqlite: %w", err)
		}

		node.Timestamp = parseFlexibleTimestamp(rawTimestamp)

		if deleted {
			continue // Skip soft-deleted nodes
		}

		hasImagesEncPrefix := imagesJSON.Valid && strings.HasPrefix(imagesJSON.String, "enc:v1:")
		if strings.HasPrefix(node.Content, "enc:v1:") || strings.HasPrefix(thought.String, "enc:v1:") || strings.HasPrefix(toolCallsJSON, "enc:v1:") || strings.HasPrefix(obsJSON, "enc:v1:") || hasImagesEncPrefix {
			node.Encrypted = true
		}

		node.Content, err = DecryptField(node.Content, s.encryptionKey)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decrypt content: %w", err)
		}

		node.Thought, err = DecryptField(thought.String, s.encryptionKey)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decrypt thought: %w", err)
		}

		node.Role = graph.Role(roleStr)

		decToolCalls, err := DecryptField(toolCallsJSON, s.encryptionKey)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decrypt tool calls: %w", err)
		}
		if err := json.Unmarshal([]byte(decToolCalls), &node.ToolCalls); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal tool calls from sqlite: %w", err)
		}

		if obsJSON != "" && obsJSON != "null" {
			decObs, err := DecryptField(obsJSON, s.encryptionKey)
			if err != nil {
				return nil, "", fmt.Errorf("failed to decrypt observations: %w", err)
			}
			if err := json.Unmarshal([]byte(decObs), &node.Observations); err != nil {
				return nil, "", fmt.Errorf("failed to unmarshal observations from sqlite: %w", err)
			}
		}
		if imagesJSON.Valid && imagesJSON.String != "" && imagesJSON.String != "null" {
			decImages, err := DecryptField(imagesJSON.String, s.encryptionKey)
			if err != nil {
				return nil, "", fmt.Errorf("failed to decrypt images: %w", err)
			}
			if err := json.Unmarshal([]byte(decImages), &node.Images); err != nil {
				return nil, "", fmt.Errorf("failed to unmarshal images from sqlite: %w", err)
			}
		}
		if err := json.Unmarshal([]byte(metadataJSON), &node.Metadata); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal metadata from sqlite: %w", err)
		}

		g.AddNode(&node)
		lastID = node.ID
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating over sqlite rows: %w", err)
	}

	return g, lastID, nil
}

// SaveSessionHead updates or creates the head node pointer for a given session.
func (s *SQLiteStorage) SaveSessionHead(sessionID, nodeID string) error {
	if sessionID == "" {
		sessionID = "main"
	}
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	query := `
	INSERT INTO sessions (id, head_node_id, updated_at)
	VALUES (?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		head_node_id = excluded.head_node_id,
		updated_at = excluded.updated_at;
	`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(query, sessionID, nodeID, now); err != nil {
		return fmt.Errorf("failed to save session head: %w", err)
	}
	return nil
}

// GetSessionHead retrieves the head node ID for a given session. Returns ("", nil) if not found.
func (s *SQLiteStorage) GetSessionHead(sessionID string) (string, error) {
	if sessionID == "" {
		sessionID = "main"
	}
	db, err := s.open()
	if err != nil {
		return "", err
	}
	defer db.Close()

	var headID string
	err = db.QueryRow("SELECT head_node_id FROM sessions WHERE id = ?", sessionID).Scan(&headID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get session head: %w", err)
	}
	return headID, nil
}

// ListSessions returns a mapping of all active session IDs to their head node IDs.
func (s *SQLiteStorage) ListSessions() (map[string]string, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, head_node_id FROM sessions ORDER BY updated_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make(map[string]string)
	for rows.Next() {
		var id, headID string
		if err := rows.Scan(&id, &headID); err != nil {
			return nil, fmt.Errorf("failed to scan session row: %w", err)
		}
		sessions[id] = headID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over session rows: %w", err)
	}
	return sessions, nil
}

// initMemoriesFTS initializes the FTS5 virtual table and synchronization triggers.
// If the SQLite engine build does not support FTS5, this fails silently and queries fall back to LIKE matching.
func initMemoriesFTS(db *sql.DB) {
	ftsQuery := `
	CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		key,
		content,
		tags,
		content='memories',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS trg_memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, key, content, tags) 
		VALUES (new.rowid, new.key, new.content, coalesce(new.tags, ''));
	END;

	CREATE TRIGGER IF NOT EXISTS trg_memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, key, content, tags) 
		VALUES ('delete', old.rowid, old.key, old.content, coalesce(old.tags, ''));
	END;

	CREATE TRIGGER IF NOT EXISTS trg_memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, key, content, tags) 
		VALUES ('delete', old.rowid, old.key, old.content, coalesce(old.tags, ''));
		INSERT INTO memories_fts(rowid, key, content, tags) 
		VALUES (new.rowid, new.key, new.content, coalesce(new.tags, ''));
	END;
	`
	_, _ = db.Exec(ftsQuery)
}

// SaveMemory inserts or updates a memory record using UPSERT semantics.
func (s *SQLiteStorage) SaveMemory(mem *Memory) error {
	if mem == nil {
		return fmt.Errorf("cannot save nil memory")
	}
	if strings.TrimSpace(mem.Key) == "" {
		return fmt.Errorf("memory key cannot be empty")
	}
	if strings.TrimSpace(mem.Content) == "" {
		return fmt.Errorf("memory content cannot be empty")
	}

	if mem.ID == "" {
		mem.ID = uuid.New().String()
	}
	if mem.Scope == "" {
		mem.Scope = ScopeWorkspace
	}
	if mem.Scope != ScopeSession {
		mem.SessionID = ""
	}
	if mem.Category == "" {
		mem.Category = CategoryFact
	}
	if mem.Confidence <= 0 {
		mem.Confidence = 1.0
	}

	now := time.Now()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	mem.UpdatedAt = now

	tagsJSON, err := json.Marshal(mem.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal memory tags: %w", err)
	}

	metadataJSON, err := json.Marshal(mem.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal memory metadata: %w", err)
	}

	encContent, err := EncryptField(mem.Content, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt memory content: %w", err)
	}

	query := `
	INSERT INTO memories (
		id, key, content, category, tags, scope, confidence, session_id, source_node_id, metadata, access_count, last_accessed_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(scope, COALESCE(session_id, ''), key) DO UPDATE SET
		content = excluded.content,
		category = excluded.category,
		tags = excluded.tags,
		confidence = excluded.confidence,
		source_node_id = CASE WHEN excluded.source_node_id != '' THEN excluded.source_node_id ELSE memories.source_node_id END,
		metadata = excluded.metadata,
		updated_at = excluded.updated_at
	`

	var lastAccessedStr interface{}
	if mem.LastAccessedAt != nil && !mem.LastAccessedAt.IsZero() {
		lastAccessedStr = mem.LastAccessedAt.Format(time.RFC3339Nano)
	}

	_, err = s.db.Exec(query,
		mem.ID,
		mem.Key,
		encContent,
		string(mem.Category),
		string(tagsJSON),
		string(mem.Scope),
		mem.Confidence,
		mem.SessionID,
		mem.SourceNodeID,
		string(metadataJSON),
		mem.AccessCount,
		lastAccessedStr,
		mem.CreatedAt.Format(time.RFC3339Nano),
		mem.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to save memory: %w", err)
	}

	return nil
}

// GetMemory retrieves a specific memory by scope, sessionID, and key.
func (s *SQLiteStorage) GetMemory(scope MemoryScope, sessionID, key string) (*Memory, error) {
	if scope == "" {
		scope = ScopeWorkspace
	}
	if scope != ScopeSession {
		sessionID = ""
	}

	query := `
	SELECT id, key, content, category, tags, scope, confidence, session_id, source_node_id, metadata, access_count, last_accessed_at, created_at, updated_at
	FROM memories
	WHERE scope = ? AND COALESCE(session_id, '') = ? AND key = ?
	`

	var mem Memory
	var encContent, catStr, tagsStr, scopeStr, metadataStr string
	var sessionIDVal, sourceNodeIDVal sql.NullString
	var lastAccessedVal sql.NullString
	var createdStr, updatedStr string

	err := s.db.QueryRow(query, string(scope), sessionID, key).Scan(
		&mem.ID,
		&mem.Key,
		&encContent,
		&catStr,
		&tagsStr,
		&scopeStr,
		&mem.Confidence,
		&sessionIDVal,
		&sourceNodeIDVal,
		&metadataStr,
		&mem.AccessCount,
		&lastAccessedVal,
		&createdStr,
		&updatedStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get memory: %w", err)
	}

	decContent, err := DecryptField(encContent, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt memory content: %w", err)
	}
	mem.Content = decContent
	mem.Category = MemoryCategory(catStr)
	mem.Scope = MemoryScope(scopeStr)
	if sessionIDVal.Valid {
		mem.SessionID = sessionIDVal.String
	}
	if sourceNodeIDVal.Valid {
		mem.SourceNodeID = sourceNodeIDVal.String
	}

	if tagsStr != "" && tagsStr != "null" {
		_ = json.Unmarshal([]byte(tagsStr), &mem.Tags)
	}
	if metadataStr != "" && metadataStr != "null" {
		_ = json.Unmarshal([]byte(metadataStr), &mem.Metadata)
	}

	mem.CreatedAt = parseFlexibleTimestamp(createdStr)
	mem.UpdatedAt = parseFlexibleTimestamp(updatedStr)
	if lastAccessedVal.Valid && lastAccessedVal.String != "" {
		t := parseFlexibleTimestamp(lastAccessedVal.String)
		mem.LastAccessedAt = &t
	}

	return &mem, nil
}

// QueryMemories retrieves memories matching the provided filter criteria.
func (s *SQLiteStorage) QueryMemories(filter MemoryFilter) ([]Memory, error) {
	var conditions []string
	var args []interface{}

	// Scope filtering
	if filter.Scope != "" && filter.Scope != "all" {
		if filter.Scope == ScopeSession {
			conditions = append(conditions, "scope = 'session' AND session_id = ?")
			args = append(args, filter.SessionID)
		} else {
			conditions = append(conditions, "scope = ?")
			args = append(args, string(filter.Scope))
		}
	} else if filter.Scope != "all" {
		// Unified Query default: global + workspace + active session (if present)
		if filter.SessionID != "" {
			conditions = append(conditions, "(scope = 'global' OR scope = 'workspace' OR (scope = 'session' AND session_id = ?))")
			args = append(args, filter.SessionID)
		} else {
			conditions = append(conditions, "(scope = 'global' OR scope = 'workspace')")
		}
	}

	// Category filtering
	if filter.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, string(filter.Category))
	}

	// Key prefix / exact matching
	if filter.Key != "" {
		if strings.Contains(filter.Key, "%") {
			conditions = append(conditions, "key LIKE ?")
			args = append(args, filter.Key)
		} else {
			conditions = append(conditions, "(key = ? OR key LIKE ?)")
			args = append(args, filter.Key, filter.Key+":%")
		}
	}

	// Full-text or substring query
	if filter.Query != "" {
		qPattern := "%" + filter.Query + "%"
		conditions = append(conditions, "(rowid IN (SELECT rowid FROM memories_fts WHERE memories_fts MATCH ?) OR key LIKE ? OR content LIKE ? OR tags LIKE ?)")
		args = append(args, filter.Query, qPattern, qPattern, qPattern)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
	SELECT id, key, content, category, tags, scope, confidence, session_id, source_node_id, metadata, access_count, last_accessed_at, created_at, updated_at
	FROM memories
	%s
	ORDER BY updated_at DESC
	LIMIT ?
	`, whereClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		// If FTS5 query failed, retry with pure LIKE conditions
		if filter.Query != "" && strings.Contains(err.Error(), "fts") {
			return s.queryMemoriesFallbackLike(filter)
		}
		return nil, fmt.Errorf("failed to query memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	var matchedIDs []string

	for rows.Next() {
		var mem Memory
		var encContent, catStr, tagsStr, scopeStr, metadataStr string
		var sessionIDVal, sourceNodeIDVal sql.NullString
		var lastAccessedVal sql.NullString
		var createdStr, updatedStr string

		if err := rows.Scan(
			&mem.ID,
			&mem.Key,
			&encContent,
			&catStr,
			&tagsStr,
			&scopeStr,
			&mem.Confidence,
			&sessionIDVal,
			&sourceNodeIDVal,
			&metadataStr,
			&mem.AccessCount,
			&lastAccessedVal,
			&createdStr,
			&updatedStr,
		); err != nil {
			return nil, fmt.Errorf("failed to scan memory row: %w", err)
		}

		decContent, err := DecryptField(encContent, s.encryptionKey)
		if err != nil {
			continue // Skip un-decryptable content
		}
		mem.Content = decContent
		mem.Category = MemoryCategory(catStr)
		mem.Scope = MemoryScope(scopeStr)
		if sessionIDVal.Valid {
			mem.SessionID = sessionIDVal.String
		}
		if sourceNodeIDVal.Valid {
			mem.SourceNodeID = sourceNodeIDVal.String
		}

		if tagsStr != "" && tagsStr != "null" {
			_ = json.Unmarshal([]byte(tagsStr), &mem.Tags)
		}
		if metadataStr != "" && metadataStr != "null" {
			_ = json.Unmarshal([]byte(metadataStr), &mem.Metadata)
		}

		mem.CreatedAt = parseFlexibleTimestamp(createdStr)
		mem.UpdatedAt = parseFlexibleTimestamp(updatedStr)
		if lastAccessedVal.Valid && lastAccessedVal.String != "" {
			t := parseFlexibleTimestamp(lastAccessedVal.String)
			mem.LastAccessedAt = &t
		}

		// Optional in-memory tag filter matching
		if len(filter.Tags) > 0 {
			hasTag := false
			for _, reqTag := range filter.Tags {
				for _, memTag := range mem.Tags {
					if strings.EqualFold(reqTag, memTag) {
						hasTag = true
						break
					}
				}
				if hasTag {
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		memories = append(memories, mem)
		matchedIDs = append(matchedIDs, mem.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating memory rows: %w", err)
	}

	// Update telemetry access timestamp only when actively recalled by agent (Touch == true)
	if filter.Touch && len(matchedIDs) > 0 {
		now := time.Now()
		nowStr := now.Format(time.RFC3339Nano)
		placeholders := strings.Repeat("?,", len(matchedIDs))
		placeholders = placeholders[:len(placeholders)-1]
		updateArgs := make([]interface{}, len(matchedIDs)+1)
		updateArgs[0] = nowStr
		for i, id := range matchedIDs {
			updateArgs[i+1] = id
		}
		_, _ = s.db.Exec(fmt.Sprintf("UPDATE memories SET access_count = access_count + 1, last_accessed_at = ? WHERE id IN (%s)", placeholders), updateArgs...)
		for i := range memories {
			memories[i].AccessCount++
			memories[i].LastAccessedAt = &now
		}
	}

	return memories, nil
}

func (s *SQLiteStorage) queryMemoriesFallbackLike(filter MemoryFilter) ([]Memory, error) {
	var conditions []string
	var args []interface{}

	if filter.Scope != "" && filter.Scope != "all" {
		if filter.Scope == ScopeSession {
			conditions = append(conditions, "scope = 'session' AND session_id = ?")
			args = append(args, filter.SessionID)
		} else {
			conditions = append(conditions, "scope = ?")
			args = append(args, string(filter.Scope))
		}
	} else if filter.Scope != "all" {
		if filter.SessionID != "" {
			conditions = append(conditions, "(scope = 'global' OR scope = 'workspace' OR (scope = 'session' AND session_id = ?))")
			args = append(args, filter.SessionID)
		} else {
			conditions = append(conditions, "(scope = 'global' OR scope = 'workspace')")
		}
	}

	if filter.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, string(filter.Category))
	}

	if filter.Key != "" {
		conditions = append(conditions, "(key = ? OR key LIKE ?)")
		args = append(args, filter.Key, filter.Key+":%")
	}

	if filter.Query != "" {
		qPattern := "%" + filter.Query + "%"
		conditions = append(conditions, "(key LIKE ? OR content LIKE ? OR tags LIKE ?)")
		args = append(args, qPattern, qPattern, qPattern)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
	SELECT id, key, content, category, tags, scope, confidence, session_id, source_node_id, metadata, access_count, last_accessed_at, created_at, updated_at
	FROM memories
	%s
	ORDER BY updated_at DESC
	LIMIT ?
	`, whereClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("fallback query failed: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var mem Memory
		var encContent, catStr, tagsStr, scopeStr, metadataStr string
		var sessionIDVal, sourceNodeIDVal sql.NullString
		var lastAccessedVal sql.NullString
		var createdStr, updatedStr string

		if err := rows.Scan(
			&mem.ID,
			&mem.Key,
			&encContent,
			&catStr,
			&tagsStr,
			&scopeStr,
			&mem.Confidence,
			&sessionIDVal,
			&sourceNodeIDVal,
			&metadataStr,
			&mem.AccessCount,
			&lastAccessedVal,
			&createdStr,
			&updatedStr,
		); err != nil {
			return nil, err
		}

		decContent, err := DecryptField(encContent, s.encryptionKey)
		if err != nil {
			continue
		}
		mem.Content = decContent
		mem.Category = MemoryCategory(catStr)
		mem.Scope = MemoryScope(scopeStr)
		if sessionIDVal.Valid {
			mem.SessionID = sessionIDVal.String
		}
		if sourceNodeIDVal.Valid {
			mem.SourceNodeID = sourceNodeIDVal.String
		}

		if tagsStr != "" && tagsStr != "null" {
			_ = json.Unmarshal([]byte(tagsStr), &mem.Tags)
		}
		if metadataStr != "" && metadataStr != "null" {
			_ = json.Unmarshal([]byte(metadataStr), &mem.Metadata)
		}

		mem.CreatedAt = parseFlexibleTimestamp(createdStr)
		mem.UpdatedAt = parseFlexibleTimestamp(updatedStr)
		if lastAccessedVal.Valid && lastAccessedVal.String != "" {
			t := parseFlexibleTimestamp(lastAccessedVal.String)
			mem.LastAccessedAt = &t
		}

		memories = append(memories, mem)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating search memories: %w", err)
	}
	return memories, nil
}

// DeleteMemory removes a memory entry by scope, sessionID, and key.
func (s *SQLiteStorage) DeleteMemory(scope MemoryScope, sessionID, key string) error {
	if scope == "" {
		scope = ScopeWorkspace
	}
	if scope != ScopeSession {
		sessionID = ""
	}

	query := `DELETE FROM memories WHERE scope = ? AND COALESCE(session_id, '') = ? AND key = ?`
	_, err := s.db.Exec(query, string(scope), sessionID, key)
	if err != nil {
		return fmt.Errorf("failed to delete memory %q: %w", key, err)
	}
	return nil
}

// DiagnoseMemories returns volume, category, staleness, and storage telemetry.
func (s *SQLiteStorage) DiagnoseMemories(scope MemoryScope, sessionID string) (*MemoryDiagnostics, error) {
	diag := &MemoryDiagnostics{
		ByScope:    make(map[MemoryScope]int),
		ByCategory: make(map[MemoryCategory]int),
	}

	whereClause := ""
	var args []interface{}
	if scope != "" && scope != "all" {
		if scope == ScopeSession {
			whereClause = "WHERE scope = 'session' AND session_id = ?"
			args = append(args, sessionID)
		} else {
			whereClause = "WHERE scope = ?"
			args = append(args, string(scope))
		}
	}

	// 1. Total counts & breakdown by scope
	scopeRows, err := s.db.Query(fmt.Sprintf("SELECT scope, COUNT(*) FROM memories %s GROUP BY scope", whereClause), args...)
	if err == nil {
		for scopeRows.Next() {
			var sc string
			var count int
			if err := scopeRows.Scan(&sc, &count); err == nil {
				diag.ByScope[MemoryScope(sc)] = count
				diag.TotalMemories += count
			}
		}
		if err := scopeRows.Err(); err != nil {
			_ = scopeRows.Close()
			return nil, fmt.Errorf("error iterating diagnostic scope rows: %w", err)
		}
		scopeRows.Close()
	}

	// 2. Breakdown by category
	catRows, err := s.db.Query(fmt.Sprintf("SELECT category, COUNT(*) FROM memories %s GROUP BY category", whereClause), args...)
	if err == nil {
		for catRows.Next() {
			var cat string
			var count int
			if err := catRows.Scan(&cat, &count); err == nil {
				diag.ByCategory[MemoryCategory(cat)] = count
			}
		}
		if err := catRows.Err(); err != nil {
			_ = catRows.Close()
			return nil, fmt.Errorf("error iterating diagnostic category rows: %w", err)
		}
		catRows.Close()
	}

	// 3. Approximate storage bytes
	_ = s.db.QueryRow(fmt.Sprintf("SELECT COALESCE(SUM(LENGTH(key) + LENGTH(content) + COALESCE(LENGTH(tags), 0) + COALESCE(LENGTH(metadata), 0)), 0) FROM memories %s", whereClause), args...).Scan(&diag.StorageBytes)

	// 4. Most accessed
	mostAcc, err := s.QueryMemories(MemoryFilter{
		Scope:     scope,
		SessionID: sessionID,
		Limit:     5,
	})
	if err == nil {
		diag.MostAccessed = mostAcc
	}

	// 5. Stale candidates (older than 14 days or access count <= 1)
	staleFilter := MemoryFilter{
		Scope:     scope,
		SessionID: sessionID,
		Limit:     5,
	}
	allMems, err := s.QueryMemories(staleFilter)
	if err == nil {
		cutoff := time.Now().Add(-14 * 24 * time.Hour)
		for _, m := range allMems {
			if m.AccessCount <= 1 || m.UpdatedAt.Before(cutoff) {
				diag.StaleCandidates = append(diag.StaleCandidates, m)
			}
		}
	}

	return diag, nil
}
