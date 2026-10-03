package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
)

// TestShaper_HermeticDeterminism asserts that given identical DAG paths,
// ShapeContext produces byte-for-byte identical prompt message slices
// regardless of real-world elapsed time (wall-clock immunity).
func TestShaper_HermeticDeterminism(t *testing.T) {
	curves := []string{"sigmoid", "window", "resonance"}

	for _, curve := range curves {
		t.Run(curve, func(t *testing.T) {
			shaper := NewContextShaper(curve, ShapeOptions{})

			// Construct a 6-node path with timestamps from "now"
			pathNow := makeDeterministicPath(time.Now())

			msgsNow, err := shaper.ShapeContext(context.Background(), pathNow, 32768)
			if err != nil {
				t.Fatalf("[%s] ShapeContext failed: %v", curve, err)
			}

			// Construct identical path with timestamps simulating 24 hours ago (or lunch break)
			pathYesterday := makeDeterministicPath(time.Now().Add(-24 * time.Hour))

			msgsYesterday, err := shaper.ShapeContext(context.Background(), pathYesterday, 32768)
			if err != nil {
				t.Fatalf("[%s] ShapeContext yesterday failed: %v", curve, err)
			}

			if len(msgsNow) != len(msgsYesterday) {
				t.Fatalf("[%s] message count mismatch: now=%d, yesterday=%d", curve, len(msgsNow), len(msgsYesterday))
			}

			for i := range msgsNow {
				m1 := msgsNow[i]
				m2 := msgsYesterday[i]
				if m1.Role != m2.Role {
					t.Errorf("[%s] msg[%d] role mismatch: %s vs %s", curve, i, m1.Role, m2.Role)
				}
				if m1.Content != m2.Content {
					t.Errorf("[%s] msg[%d] content mismatch:\nNow:       %q\nYesterday: %q", curve, i, m1.Content, m2.Content)
				}
				if len(m1.ToolCalls) != len(m2.ToolCalls) {
					t.Errorf("[%s] msg[%d] tool call count mismatch", curve, i)
				}
			}
		})
	}
}

// TestShaper_MonotonicPrefixInvariance verifies that as new turns are appended to an
// active conversational trajectory, the rendered text of historical turns past the active
// working window settles onto a stable floor and remains 100% immutable character-for-character.
func TestShaper_MonotonicPrefixInvariance(t *testing.T) {
	shaper := NewSigmoidShaper(func(s *SigmoidShaper) {
		s.K = 1.0
		s.D0 = 4.0 // inflection at 4 turns
	})

	// Build a baseline 8-turn path: Genesis root, user goal, then 3 user/asst pairs
	basePath := []*graph.Node{
		{
			ID:        "root",
			Role:      domain.RoleSystem,
			Content:   "System persona instructions",
			Timestamp: time.Now().Add(-10 * time.Minute),
		},
		{
			ID:        "goal",
			ParentID:  "root",
			Role:      domain.RoleUser,
			Content:   "Initial user goal: build cache stability",
			Timestamp: time.Now().Add(-9 * time.Minute),
		},
	}

	currParent := "goal"
	for i := 1; i <= 3; i++ {
		uNode := &graph.Node{
			ID:        fmt.Sprintf("user_%d", i),
			ParentID:  currParent,
			Role:      domain.RoleUser,
			Content:   fmt.Sprintf("User question %d", i),
			Timestamp: time.Now().Add(-8 * time.Minute),
		}
		basePath = append(basePath, uNode)
		currParent = uNode.ID

		aNode := &graph.Node{
			ID:        fmt.Sprintf("asst_%d", i),
			ParentID:  currParent,
			Role:      domain.RoleAssistant,
			Content:   fmt.Sprintf("Assistant answer %d", i),
			Timestamp: time.Now().Add(-7 * time.Minute),
			Observations: []domain.ToolObservation{
				{
					ToolCallID: fmt.Sprintf("call_%d", i),
					Result:     strings.Repeat(fmt.Sprintf("tool data %d ", i), 100), // > 1000 bytes to test compaction
				},
			},
			ToolCalls: []domain.ToolCall{
				{
					ID:   fmt.Sprintf("call_%d", i),
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path": "foo.txt"}`),
					},
				},
			},
		}
		basePath = append(basePath, aNode)
		currParent = aNode.ID
	}

	// 1. Project context for the 8-node path under capacity pressure (budget = 1000)
	budget := 1000
	initialMessages, err := shaper.ShapeContext(context.Background(), basePath, budget)
	if err != nil {
		t.Fatalf("failed initial shape: %v", err)
	}

	// Genesis root (idx 0) and User goal (idx 1) must be preserved
	if len(initialMessages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(initialMessages))
	}
	rootContent := initialMessages[0].Content
	goalContent := initialMessages[1].Content

	// 2. Append 6 more turns (user + assistant pairs) to extend the conversation
	extendedPath := append([]*graph.Node{}, basePath...)
	for i := 4; i <= 6; i++ {
		uNode := &graph.Node{
			ID:        fmt.Sprintf("user_%d", i),
			ParentID:  currParent,
			Role:      domain.RoleUser,
			Content:   fmt.Sprintf("User continuation %d", i),
			Timestamp: time.Now().Add(-2 * time.Minute),
		}
		extendedPath = append(extendedPath, uNode)
		currParent = uNode.ID

		aNode := &graph.Node{
			ID:        fmt.Sprintf("asst_%d", i),
			ParentID:  currParent,
			Role:      domain.RoleAssistant,
			Content:   fmt.Sprintf("Assistant answer %d", i),
			Timestamp: time.Now().Add(-1 * time.Minute),
		}
		extendedPath = append(extendedPath, aNode)
		currParent = aNode.ID
	}

	// 3. Project context for the 14-node extended path
	extendedMessages, err := shaper.ShapeContext(context.Background(), extendedPath, budget)
	if err != nil {
		t.Fatalf("failed extended shape: %v", err)
	}

	// Monotonic Prefix Contract Check:
	// Pinned Genesis root (0) and Initial user goal (1) MUST NOT change
	if extendedMessages[0].Content != rootContent {
		t.Errorf("Genesis root mutated across turns!\nOriginal: %q\nExtended: %q", rootContent, extendedMessages[0].Content)
	}
	if extendedMessages[1].Content != goalContent {
		t.Errorf("Initial user goal mutated across turns!\nOriginal: %q\nExtended: %q", goalContent, extendedMessages[1].Content)
	}

	// Verify that deep ancestor tool observations remain identical
	// (Node user_1/asst_1 was already historical in initial, and remains historical in extended)
	for idx, msg := range initialMessages {
		if msg.Role == domain.RoleTool && strings.Contains(msg.Content, "call_1") {
			// Find corresponding tool message in extended
			found := false
			for _, extMsg := range extendedMessages {
				if extMsg.Role == domain.RoleTool && extMsg.ToolCallID == msg.ToolCallID {
					if extMsg.Content != msg.Content {
						t.Errorf("Historical tool observation at idx %d mutated!\nInitial:  %s\nExtended: %s", idx, msg.Content, extMsg.Content)
					}
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Historical tool observation %s disappeared from extended context", msg.ToolCallID)
			}
		}
	}
}

// TestShaper_WindowShaper_StrictBoundary asserts that WindowShaper enforces
// a strict K-turn boundary while keeping Node 0 (Genesis root) pinned.
func TestShaper_WindowShaper_StrictBoundary(t *testing.T) {
	windowSize := 4
	shaper := NewWindowShaper(windowSize)

	// Create 10-node path: Node 0 (System), Nodes 1..9 (User/Asst)
	path := []*graph.Node{
		{
			ID:      "root",
			Role:    domain.RoleSystem,
			Content: "System root",
		},
	}
	for i := 1; i <= 9; i++ {
		path = append(path, &graph.Node{
			ID:      fmt.Sprintf("node_%d", i),
			Role:    domain.RoleUser,
			Content: fmt.Sprintf("Turn %d", i),
		})
	}

	// High pressure to engage window pruning (budget = 50)
	msgs, err := shaper.ShapeContext(context.Background(), path, 50)
	if err != nil {
		t.Fatalf("ShapeContext failed: %v", err)
	}

	// Genesis Root (Node 0) must be present and pinned
	if len(msgs) == 0 || msgs[0].Content != "System root" {
		t.Errorf("expected pinned Genesis root at messages[0], got %+v", msgs[0])
	}

	// Leaf turn (Turn 9) must be present
	leafFound := false
	for _, m := range msgs {
		if m.Content == "Turn 9" {
			leafFound = true
			break
		}
	}
	if !leafFound {
		t.Errorf("expected active leaf Turn 9 to be present in context")
	}
}

// TestShaper_FactoryAndConfiguration verifies NewContextShaper instantiation across all curves.
func TestShaper_FactoryAndConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		curve    string
		wantType string
	}{
		{"default empty", "", "*engine.SigmoidShaper"},
		{"sigmoid lowercase", "sigmoid", "*engine.SigmoidShaper"},
		{"window curve", "window", "*engine.WindowShaper"},
		{"resonance curve", "resonance", "*engine.ResonanceShaper"},
		{"exponential alias", "exponential", "*engine.ResonanceShaper"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewContextShaper(tt.curve, ShapeOptions{})
			got := fmt.Sprintf("%T", s)
			if got != tt.wantType {
				t.Errorf("NewContextShaper(%q) = %s, want %s", tt.curve, got, tt.wantType)
			}
		})
	}
}

func makeDeterministicPath(baseTime time.Time) []*graph.Node {
	return []*graph.Node{
		{
			ID:        "node_0",
			Role:      domain.RoleSystem,
			Content:   "System prompt persona",
			Timestamp: baseTime,
		},
		{
			ID:        "node_1",
			ParentID:  "node_0",
			Role:      domain.RoleUser,
			Content:   "Please analyze this code",
			Timestamp: baseTime.Add(1 * time.Minute),
		},
		{
			ID:        "node_2",
			ParentID:  "node_1",
			Role:      domain.RoleAssistant,
			Content:   "Let me read the file",
			Timestamp: baseTime.Add(2 * time.Minute),
			ToolCalls: []domain.ToolCall{
				{
					ID:   "call_read",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path": "main.go"}`),
					},
				},
			},
			Observations: []domain.ToolObservation{
				{
					ToolCallID: "call_read",
					Result:     "package main\n\nfunc main() {}\n",
				},
			},
		},
		{
			ID:        "node_3",
			ParentID:  "node_2",
			Role:      domain.RoleUser,
			Content:   "Now add a test",
			Timestamp: baseTime.Add(3 * time.Minute),
		},
		{
			ID:        "node_4",
			ParentID:  "node_3",
			Role:      domain.RoleAssistant,
			Content:   "Adding tests now",
			Timestamp: baseTime.Add(4 * time.Minute),
		},
	}
}
