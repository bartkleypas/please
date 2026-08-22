package server

import (
	"fmt"
	"sync"
	"time"
)

// DaemonEventType defines types of global DAG mutations broadcast to subscribers.
type DaemonEventType string

const (
	EventNodeSaved       DaemonEventType = "node_saved"
	EventBranchPruned    DaemonEventType = "branch_pruned"
	EventBranchCompacted DaemonEventType = "branch_compacted"
	EventStreamStarted   DaemonEventType = "stream_started"
)

// DaemonEvent represents a structured notification sent over SSE.
type DaemonEvent struct {
	Type      DaemonEventType        `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

// EventSubscription provides a channel for a subscriber and an unsubscribe func.
type EventSubscription struct {
	ID      string
	Events  chan DaemonEvent
	bus     *EventBus
	closing sync.Once
}

// Close unregisters the subscription from the EventBus.
func (s *EventSubscription) Close() {
	s.closing.Do(func() {
		s.bus.unsubscribe(s.ID)
		close(s.Events)
	})
}

// EventBus manages thread-safe subscriber channels with non-blocking broadcast.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]chan DaemonEvent
	nextID      uint64
}

// NewEventBus initializes a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]chan DaemonEvent),
	}
}

// Subscribe registers a new subscriber channel with a buffer.
func (b *EventBus) Subscribe() *EventSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), b.nextID)
	ch := make(chan DaemonEvent, 64)
	b.subscribers[id] = ch

	return &EventSubscription{
		ID:     id,
		Events: ch,
		bus:    b,
	}
}

func (b *EventBus) unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, id)
}

// SubscriberCount returns current active subscriber count.
func (b *EventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Publish broadcasts an event to all active subscribers.
// Delivery is non-blocking: if a subscriber's buffer is full, the event is dropped for that subscriber.
func (b *EventBus) Publish(eventType DaemonEventType, payload map[string]interface{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.subscribers) == 0 {
		return
	}

	event := DaemonEvent{
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Non-blocking drop for lagging subscribers
		}
	}
}
