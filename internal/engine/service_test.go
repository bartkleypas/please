package engine

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestManager_Validation(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	tests := []struct {
		name    string
		fn      func() error
		wantErr string
	}{
		{
			name: "Empty user content",
			fn: func() error {
				_, err := mgr.CreateNode("root", RoleUser, "", false)
				return err
			},
			wantErr: "user message content cannot be empty",
		},
		{
			name: "Self-parenting node",
			fn: func() error {
				// We have to bypass the UUID generation to test ID == ParentID
				// Since CreateNode generates a UUID, we'll test the internal validateNode directly
				node := &Node{ID: "A", ParentID: "A", Role: RoleUser, Content: "Hello"}
				return mgr.validateNode(node)
			},
			wantErr: "node cannot be its own parent",
		},
		{
			name: "Tool node missing ToolCallID",
			fn: func() error {
				_, err := mgr.CreateToolNode("root", "", "result", false)
				return err
			},
			wantErr: "tool node must have a ToolCallID",
		},
		{
			name: "Tool node empty content",
			fn: func() error {
				_, err := mgr.CreateToolNode("root", "call_123", "", false)
				return err
			},
			wantErr: "tool result content cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Error("expected error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// MockStorage for testing
type MockStorage struct{}

func (s *MockStorage) SaveNode(n *Node) error                                        { return nil }
func (s *MockStorage) LoadGraph() (*Graph, string, error)                            { return NewGraph(), "", nil }
func (s *MockStorage) UpdateNodeMetadata(n *Node) error                              { return nil }
func (s *MockStorage) UpdateNodeParentID(id, p string) error                         { return nil }
func (s *MockStorage) UpdateNodeObservations(id string, obs []ToolObservation) error { return nil }
func (s *MockStorage) GarbageCollect() (int64, error)                                { return 0, nil }
func (s *MockStorage) Close() error                                                  { return nil }
func (s *MockStorage) Vacuum() error                                                 { return nil }

func TestManager_ResonanceScoring(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	// 1. Create a root system node
	systemNode, _ := mgr.CreateNode("", RoleSystem, "System prompt", false)

	// 2. Test MaxFloat64 for RoleSystem and RoleSummary
	score := mgr.calculateResonanceScore(systemNode, 10)
	if score != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for system node, got %f", score)
	}

	summaryNode := &Node{
		ID:      "summary_1",
		Role:    RoleSummary,
		Content: "Compacted discussion",
	}
	score = mgr.calculateResonanceScore(summaryNode, 10)
	if score != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for summary node, got %f", score)
	}

	// 3. Test Grace Window: distance < 3
	// We create a node from 2 hours ago
	oldTime := time.Now().Add(-2 * time.Hour)
	oldUserNode := &Node{
		ID:        "old_user",
		Role:      RoleUser,
		Content:   "Hello",
		Timestamp: oldTime,
	}

	// At distance = 1 (inside grace window), it should have no decay.
	// Base score should be weight (1.0) * 10000.0 / cost (5) = 2000.0
	scoreGrace := mgr.calculateResonanceScore(oldUserNode, 1)
	if math.Abs(scoreGrace-2000.0) > 1e-9 {
		t.Errorf("expected base score of 2000.0 (no decay in grace window), got %f", scoreGrace)
	}

	// At distance = 3 (outside grace window), it should experience both time and turn decay.
	// turnsPastGrace = 3 - 3 + 1 = 1.
	// deltaMinutes = 120.
	// decay = e^(-0.02 * 120) * e^(-0.1 * 1) = e^(-2.4) * e^(-0.1) = e^(-2.5) ≈ 0.082085
	// expected = 2000.0 * e^(-2.5) ≈ 164.17
	scoreDecayed := mgr.calculateResonanceScore(oldUserNode, 3)
	expectedScore := 2000.0 * math.Exp(-0.02*120.0) * math.Exp(-0.1*1.0)
	if math.Abs(scoreDecayed-expectedScore) > 1e-2 {
		t.Errorf("expected decayed score around %f, got %f", expectedScore, scoreDecayed)
	}
}

func TestManager_BuildLLMContext_FidelityAndToolSummaries(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	// Construct a path of nodes standardly
	// 1. Root system node
	n1, _ := mgr.CreateNode("", RoleSystem, "System Prompt", false)

	// 2. Assistant node with large tool calls
	tcs := []ToolCall{
		{
			ID:   "call_abc",
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path": "config.go"}`),
			},
		},
	}

	n2, err := mgr.CreateAssistantNode(n1.ID, "Executing read_file...", "", tcs, false)
	if err != nil {
		t.Fatalf("failed to create assistant node 2: %v", err)
	}

	// Add massive observation to force low fidelity
	err = mgr.UpdateAssistantObservations(n2.ID, "call_abc", strings.Repeat("some large output content ", 2000))
	if err != nil {
		t.Fatalf("failed to update observations: %v", err)
	}

	// 3. User node (distance = 2)
	n3, _ := mgr.CreateNode(n2.ID, RoleUser, "Thanks", false)

	// 4. Internal assistant node (distance = 1) - should be dropped completely if low fidelity
	n4, err := mgr.CreateAssistantNode(n3.ID, strings.Repeat("internal thought trace ", 300), "", nil, true)
	if err != nil {
		t.Fatalf("failed to create assistant node 4: %v", err)
	}

	// 5. Leaf assistant node (distance = 0)
	n5, _ := mgr.CreateNode(n4.ID, RoleAssistant, "Final answer", false)

	// Build context from leaf n5
	messages, err := mgr.BuildLLMContext(n5.ID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	// Verify path reconstruction size
	// We expect:
	// - System node (retained)
	// - Node 2 (retained: split into 1 assistant message and 1 tool message because it has segments)
	// - Node 3 (retained)
	// - Node 4 (internal, low fidelity, so DROPPED entirely!)
	// - Node 5 (active leaf, retained in high fidelity)
	// Total expected messages = 5
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages in context, got %d", len(messages))
	}

	// Check if internal node (Node 4) was indeed dropped
	for _, msg := range messages {
		if msg.Internal {
			t.Error("expected internal low-fidelity node to be dropped, but it was present")
		}
	}

	// Check Node 2's pruned observation summary
	foundSummary := false
	for _, msg := range messages {
		if msg.Role == RoleTool && msg.ToolCallID == "call_abc" {
			obsResult := msg.Content
			if !strings.Contains(obsResult, "[Tool 'read_file' execution completed. Detailed results omitted. Total size:") {
				t.Errorf("expected informative summary, got: %s", obsResult)
			}
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Error("could not find pruned tool observation message")
	}
}

func TestManager_BuildLLMContext_SequentialSegments(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	// 1. Create root node
	n1, _ := mgr.CreateNode("", RoleUser, "Run test", false)

	// 2. Create assistant node with first tool call
	tcs1 := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      "test_tool",
				Arguments: json.RawMessage(`{"arg": 1}`),
			},
		},
	}
	n2, err := mgr.CreateAssistantNode(n1.ID, "", "thought 1", tcs1, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}

	// 3. Update with observation 1
	err = mgr.UpdateAssistantObservations(n2.ID, "call_1", "success 1")
	if err != nil {
		t.Fatalf("failed to update observation 1: %v", err)
	}

	// 4. Simulate stream finish for second segment
	// Retrieve node to update segments metadata
	node, err := mgr.GetNode(n2.ID)
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}
	node.Content += "Step 1 done. Running test 2."
	node.Thought += "thought 2"
	tcs2 := []ToolCall{
		{
			ID:   "call_2",
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      "test_tool",
				Arguments: json.RawMessage(`{"arg": 2}`),
			},
		},
	}
	node.ToolCalls = append(node.ToolCalls, tcs2...)

	// Update segments in metadata manually to match the streaming behavior
	var segments []AssistantSegment
	if segStr, ok := node.Metadata["segments"]; ok && segStr != "" {
		_ = json.Unmarshal([]byte(segStr), &segments)
	}
	segments = append(segments, AssistantSegment{
		Content: "Step 1 done. Running test 2.",
		Thought: "thought 2",
	})
	segJSON, _ := json.Marshal(segments)
	node.Metadata["segments"] = string(segJSON)

	// Update observation 2
	node.Observations = append(node.Observations, ToolObservation{
		ToolCallID: "call_2",
		Result:     "success 2",
	})

	// Save back
	err = mgr.Storage.SaveNode(node)
	if err != nil {
		t.Fatalf("failed to save node: %v", err)
	}

	// 5. Build context
	messages, err := mgr.BuildLLMContext(n2.ID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	// Expected messages:
	// - User: "Run test" (1 message)
	// - Assistant: (Segment 0) Content "", Thought "thought 1", ToolCalls ["call_1"] (2 message)
	// - Tool: "success 1", ToolCallID "call_1" (3 message)
	// - Assistant: (Segment 1) Content "Step 1 done...", Thought "thought 2", ToolCalls ["call_2"] (4 message)
	// - Tool: "success 2", ToolCallID "call_2" (5 message)
	// Total expected = 5 messages
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(messages))
	}

	if messages[0].Role != RoleUser || messages[0].Content != "Run test" {
		t.Errorf("unexpected message 0: %+v", messages[0])
	}
	if messages[1].Role != RoleAssistant || messages[1].Content != "" || messages[1].Thought != "thought 1" || len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("unexpected message 1: %+v", messages[1])
	}
	if messages[2].Role != RoleTool || messages[2].Content != "success 1" || messages[2].ToolCallID != "call_1" {
		t.Errorf("unexpected message 2: %+v", messages[2])
	}
	if messages[3].Role != RoleAssistant || messages[3].Content != "Step 1 done. Running test 2." || messages[3].Thought != "thought 2" || len(messages[3].ToolCalls) != 1 || messages[3].ToolCalls[0].ID != "call_2" {
		t.Errorf("unexpected message 3: %+v", messages[3])
	}
	if messages[4].Role != RoleTool || messages[4].Content != "success 2" || messages[4].ToolCallID != "call_2" {
		t.Errorf("unexpected message 4: %+v", messages[4])
	}
}

func TestCreateNode_RoleToolAutoGeneratedID(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	node, err := mgr.CreateNode("root", RoleTool, "tool output", false)
	if err != nil {
		t.Fatalf("expected no error creating tool node, got: %v", err)
	}

	if node.ToolCallID == "" {
		t.Error("expected ToolCallID to be auto-generated, got empty string")
	}

	if !strings.HasPrefix(node.ToolCallID, "cli_") {
		t.Errorf("expected ToolCallID to start with 'cli_', got: %s", node.ToolCallID)
	}
}

func TestBuildLLMContext_ImageFallbackFlow(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	sdPrompt := "cybernetic banana prompt\nSeed: 98765, Model: Stable Banana"
	sdImgPath := createTestPNG(t, sdPrompt)
	defer os.Remove(sdImgPath)

	normalImgPath := createTestPNG(t, "")
	defer os.Remove(normalImgPath)

	node, err := mgr.CreateNode("", RoleUser, "Draw this", false)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	mgr.AttachImages(node, []string{sdImgPath, normalImgPath})
	_ = mgr.Storage.SaveNode(node)

	// --- Case 1: Vision Enabled ---
	messagesVision, err := mgr.BuildLLMContext(node.ID, true)
	if err != nil {
		t.Fatalf("failed to build vision context: %v", err)
	}
	if len(messagesVision) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messagesVision))
	}
	msgV := messagesVision[0]
	if len(msgV.Images) != 2 {
		t.Errorf("expected 2 images in vision payload, got %d", len(msgV.Images))
	}
	if msgV.Images[0] != sdImgPath || msgV.Images[1] != normalImgPath {
		t.Errorf("unexpected image paths: %+v", msgV.Images)
	}
	if !strings.Contains(msgV.Content, "cybernetic banana prompt") {
		t.Errorf("expected vision message content to contain SD prompt context, got: %s", msgV.Content)
	}

	// --- Case 2: Vision Disabled (Fallback) ---
	messagesText, err := mgr.BuildLLMContext(node.ID, false)
	if err != nil {
		t.Fatalf("failed to build text fallback context: %v", err)
	}
	if len(messagesText) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messagesText))
	}
	msgT := messagesText[0]
	if len(msgT.Images) != 0 {
		t.Errorf("expected 0 images in text fallback payload, got %d", len(msgT.Images))
	}
	if !strings.Contains(msgT.Content, "cybernetic banana prompt") {
		t.Errorf("expected fallback content to contain SD prompt context, got: %s", msgT.Content)
	}
	if !strings.Contains(msgT.Content, "No SD metadata available") {
		t.Errorf("expected fallback content to contain normal image placeholder, got: %s", msgT.Content)
	}
}

func createTestPNG(t *testing.T, params string) string {
	f, err := os.CreateTemp("", "test_img_*.png")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	writeChunk(&buf, "IHDR", make([]byte, 13))

	if params != "" {
		var textData bytes.Buffer
		textData.WriteString("parameters")
		textData.WriteByte(0)
		textData.WriteString(params)
		writeChunk(&buf, "tEXt", textData.Bytes())
	}
	writeChunk(&buf, "IEND", nil)

	_, err = f.Write(buf.Bytes())
	if err != nil {
		t.Fatalf("failed to write mock PNG bytes: %v", err)
	}
	return f.Name()
}
