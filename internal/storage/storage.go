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
