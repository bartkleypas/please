package engine

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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
}

// SQLiteStorage implements Storage using an SQLite database
type SQLiteStorage struct {
	DBPath string
	db     *sql.DB
}

// NewSQLiteStorage creates a new instance of SQLiteStorage and initializes the schema
func NewSQLiteStorage(path string) (*SQLiteStorage, error) {
	s := &SQLiteStorage{DBPath: path}
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
		timestamp DATETIME,
		tool_calls TEXT,
		tool_call_id TEXT,
		metadata TEXT,
		deleted BOOLEAN DEFAULT 0
	);
	`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("failed to initialize sqlite schema: %w", err)
	}

	// Migration: Add deleted column if it doesn't exist (ignoring errors if it does)
	_, _ = db.Exec("ALTER TABLE nodes ADD COLUMN deleted BOOLEAN DEFAULT 0")

	return s, nil
}

func (s *SQLiteStorage) open() (*sql.DB, error) {
	db, err := sql.Open("sqlite", s.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

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

	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
	INSERT INTO nodes (id, parent_id, role, content, timestamp, tool_calls, tool_call_id, metadata, deleted)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		node.ID,
		node.ParentID,
		string(node.Role),
		node.Content,
		node.Timestamp,
		string(toolCallsJSON),
		node.ToolCallID,
		string(metadataJSON),
		node.Deleted,
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

// LoadGraph reads all nodes from the SQLite database and reconstructs the Graph.
func (s *SQLiteStorage) LoadGraph() (*Graph, string, error) {
	query := `SELECT id, parent_id, role, content, timestamp, tool_calls, tool_call_id, metadata, deleted FROM nodes ORDER BY timestamp ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query nodes from sqlite: %w", err)
	}
	defer rows.Close()

	graph := NewGraph()
	var lastID string

	for rows.Next() {
		var node Node
		var roleStr, toolCallsJSON, metadataJSON string

		err := rows.Scan(
			&node.ID,
			&node.ParentID,
			&roleStr,
			&node.Content,
			&node.Timestamp,
			&toolCallsJSON,
			&node.ToolCallID,
			&metadataJSON,
			&node.Deleted,
		)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan node from sqlite: %w", err)
		}

		if node.Deleted {
			continue // Skip soft-deleted nodes
		}

		node.Role = Role(roleStr)
		if err := json.Unmarshal([]byte(toolCallsJSON), &node.ToolCalls); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal tool calls from sqlite: %w", err)
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
	FilePath string
}

// NewJSONLStorage creates a new instance of JSONLStorage
func NewJSONLStorage(path string) *JSONLStorage {
	return &JSONLStorage{FilePath: path}
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
