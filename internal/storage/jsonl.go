package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
)

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

func (s *JSONLStorage) UpdateNodeMetadata(node *graph.Node) error {
	return fmt.Errorf("metadata updates not implemented for JSONL storage")
}

func (s *JSONLStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	return fmt.Errorf("parent updates not implemented for JSONL storage")
}

func (s *JSONLStorage) UpdateNodeObservations(nodeID string, obs []providers.ToolObservation) error {
	return fmt.Errorf("observation updates not implemented for JSONL storage")
}

// SaveNode appends a single node to the JSONL file
func (s *JSONLStorage) SaveNode(node *graph.Node) error {
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
func (s *JSONLStorage) LoadGraph() (*graph.Graph, string, error) {
	file, err := os.Open(s.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return graph.NewGraph(), "", nil // Return empty graph and empty ID if file doesn't exist yet
		}
		return nil, "", fmt.Errorf("failed to open storage file: %w", err)
	}
	defer file.Close()

	// Obtain a shared lock on the file for reading
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH); err != nil {
		return nil, "", fmt.Errorf("failed to lock storage file: %w", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	g := graph.NewGraph()
	var lastID string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var node graph.Node
		if err := json.Unmarshal(scanner.Bytes(), &node); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal node: %w", err)
		}
		g.AddNode(&node)
		lastID = node.ID
	}

	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("error reading storage file: %w", err)
	}

	return g, lastID, nil
}
