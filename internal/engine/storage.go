package engine

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// Storage defines the interface for persisting the conversation graph
type Storage interface {
	SaveNode(node *Node) error
	LoadGraph() (*Graph, string, error)
	GarbageCollect() (int64, error)
	UpdateNodeMetadata(node *Node) error
	UpdateNodeParentID(nodeID, newParentID string) error
	UpdateNodeObservations(nodeID string, obs []ToolObservation) error
}

// SQLiteStorage implements Storage using an SQLite database
type SQLiteStorage struct {
	DBPath        string
	encryptionKey string
	db            *sql.DB
}

// NewSQLiteStorage creates a new instance of SQLiteStorage and initializes the schema
func NewSQLiteStorage(path, key string) (*SQLiteStorage, error) {
	s := &SQLiteStorage{DBPath: path, encryptionKey: key}
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
	`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("failed to initialize sqlite schema: %w", err)
	}

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
func (s *SQLiteStorage) SaveNode(node *Node) error {
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
	INSERT OR REPLACE INTO nodes (id, parent_id, role, content, thought, timestamp, tool_calls, tool_call_id, observations, metadata, deleted, internal, images)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		node.ID,
		node.ParentID,
		string(node.Role),
		encContent,
		encThought,
		node.Timestamp,
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
func (s *SQLiteStorage) UpdateNodeMetadata(node *Node) error {
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
	res, err := s.db.Exec("DELETE FROM nodes WHERE deleted = 1")
	if err != nil {
		return 0, fmt.Errorf("failed to delete nodes: %w", err)
	}

	rows, _ := res.RowsAffected()

	_, err = s.db.Exec("VACUUM")
	if err != nil {
		return rows, fmt.Errorf("vacuum failed: %w", err)
	}

	return rows, nil
}

// UpdateNodeParentID updates the parent reference of a node in the database
func (s *SQLiteStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	query := `UPDATE nodes SET parent_id = ? WHERE id = ?`
	_, err := s.db.Exec(query, newParentID, nodeID)
	if err != nil {
		return fmt.Errorf("failed to update node parent in sqlite: %w", err)
	}
	return nil
}

// UpdateNodeObservations updates the side-channel results of a node in the database
func (s *SQLiteStorage) UpdateNodeObservations(nodeID string, obs []ToolObservation) error {
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
func (s *SQLiteStorage) LoadGraph() (*Graph, string, error) {
	query := `SELECT id, parent_id, role, content, thought, timestamp, tool_calls, tool_call_id, observations, metadata, deleted, internal, images FROM nodes ORDER BY timestamp ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query nodes from sqlite: %w", err)
	}
	defer rows.Close()

	graph := NewGraph()
	var lastID string

	for rows.Next() {
		var node Node
		var roleStr, toolCallsJSON, obsJSON, metadataJSON string
		var imagesJSON sql.NullString
		var thought sql.NullString
		var deleted bool

		err := rows.Scan(
			&node.ID,
			&node.ParentID,
			&roleStr,
			&node.Content,
			&thought,
			&node.Timestamp,
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

		node.Role = Role(roleStr)

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

		graph.AddNode(&node)
		lastID = node.ID
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating over sqlite rows: %w", err)
	}

	return graph, lastID, nil
}

// JSONLStorage implements Storage using a JSON Lines file
type JSONLStorage struct {
	FilePath      string
	encryptionKey string
}

// NewJSONLStorage creates a new instance of JSONLStorage
func NewJSONLStorage(path, key string) *JSONLStorage {
	return &JSONLStorage{FilePath: path, encryptionKey: key}
}

func (s *JSONLStorage) GarbageCollect() (int64, error) {
	return 0, fmt.Errorf("garbage collection not implemented for JSONL storage")
}

func (s *JSONLStorage) UpdateNodeMetadata(node *Node) error {
	return fmt.Errorf("metadata updates not implemented for JSONL storage")
}

func (s *JSONLStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	return fmt.Errorf("parent updates not implemented for JSONL storage")
}

func (s *JSONLStorage) UpdateNodeObservations(nodeID string, obs []ToolObservation) error {
	return fmt.Errorf("observation updates not implemented for JSONL storage")
}

// SaveNode appends a single node to the JSONL file
func (s *JSONLStorage) SaveNode(node *Node) error {
	file, err := os.OpenFile(s.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open storage file: %w", err)
	}
	defer file.Close()

	// Obtain an exclusive lock on the file
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to lock storage file: %w", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write node to file: %w", err)
	}

	return nil
}

// LoadGraph reads the JSONL file and reconstructs the Graph.
// It returns the graph and the ID of the last node encountered in the file.
func (s *JSONLStorage) LoadGraph() (*Graph, string, error) {
	file, err := os.Open(s.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewGraph(), "", nil // Return empty graph and empty ID if file doesn't exist yet
		}
		return nil, "", fmt.Errorf("failed to open storage file: %w", err)
	}
	defer file.Close()

	// Obtain a shared lock on the file for reading
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH); err != nil {
		return nil, "", fmt.Errorf("failed to lock storage file: %w", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	graph := NewGraph()
	var lastID string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var node Node
		if err := json.Unmarshal(scanner.Bytes(), &node); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal node: %w", err)
		}
		graph.AddNode(&node)
		lastID = node.ID
	}

	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("error reading storage file: %w", err)
	}

	return graph, lastID, nil
}
