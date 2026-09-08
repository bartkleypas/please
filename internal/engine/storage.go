package engine

import (
	"github.com/bartkleypas/please/internal/storage"
)

// Storage defines the interface for persisting the conversation graph (forwarded from internal/storage)
type Storage = storage.Storage

// SQLiteStorage implements Storage using an SQLite database in WAL mode (forwarded from internal/storage)
type SQLiteStorage = storage.SQLiteStorage

// JSONLStorage implements Storage using a JSON Lines file (forwarded from internal/storage)
type JSONLStorage = storage.JSONLStorage

// Forwarded constructors
var (
	// NewSQLiteStorage creates a new instance of SQLiteStorage
	NewSQLiteStorage = storage.NewSQLiteStorage

	// NewJSONLStorage creates a new instance of JSONLStorage
	NewJSONLStorage = storage.NewJSONLStorage
)
