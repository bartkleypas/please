package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	score := mgr.calculateResonanceScore(systemNode, 10, 0.90, 10)
	if score != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for system node, got %f", score)
	}

	summaryNode := &Node{
		ID:      "summary_1",
		Role:    RoleSummary,
		Content: "Compacted discussion",
	}
	score = mgr.calculateResonanceScore(summaryNode, 10, 0.90, 10)
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
	// Base score should be 20.0 (capped max base score)
	scoreGrace := mgr.calculateResonanceScore(oldUserNode, 1, 0.90, 10)
	if math.Abs(scoreGrace-20.0) > 1e-9 {
		t.Errorf("expected base score of 20.0 (no decay in grace window), got %f", scoreGrace)
	}

	// At distance = 3 (outside grace window), it should experience both time and turn decay.
	// turnsPastGrace = 3 - 3 + 1 = 1.
	// deltaMinutes = 120.
	// decay = e^(-0.02 * 120) * e^(-0.3 * 1) = e^(-2.4) * e^(-0.3) = e^(-2.7)
	scoreDecayed := mgr.calculateResonanceScore(oldUserNode, 3, 0.90, 10)
	expectedScore := 20.0 * math.Exp(-0.02*120.0) * math.Exp(-0.3*1.0)
	if math.Abs(scoreDecayed-expectedScore) > 1e-2 {
		t.Errorf("expected decayed score around %f, got %f", expectedScore, scoreDecayed)
	}
}

func TestManager_BuildLLMContext_FidelityAndToolSummaries(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})
	mgr.NumCtx = 4000 // Force high pressure to test observation crushing

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

func TestBuildLLMContext_BudgetAwareRetention(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})
	mgr.NumCtx = 131072 // 128k context window (e.g. Gemma 4)

	// Create 8 deep conversational turns with thoughts
	root, _ := mgr.CreateNode("", RoleSystem, "System prompt", false)
	currID := root.ID

	for i := 1; i <= 8; i++ {
		user, _ := mgr.CreateNode(currID, RoleUser, fmt.Sprintf("User question %d", i), false)
		asst, _ := mgr.CreateAssistantNode(user.ID, fmt.Sprintf("Answer %d", i), fmt.Sprintf("Reasoning thought for turn %d", i), nil, false)
		currID = asst.ID
	}

	// 1. With 128k context, fillRatio < 60% -> All 8 assistant turns MUST retain their thoughts
	messages, err := mgr.BuildLLMContext(currID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	asstCount := 0
	thoughtsRetained := 0
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			asstCount++
			if msg.Thought != "" {
				thoughtsRetained++
			}
		}
	}

	if asstCount != 8 {
		t.Errorf("expected 8 assistant messages, got %d", asstCount)
	}
	if thoughtsRetained != 8 {
		t.Errorf("expected all 8 assistant turns to retain thoughts under 128k headroom, got %d", thoughtsRetained)
	}

	// 2. Test tight context capacity (e.g. NumCtx = 50 tokens) -> High pressure triggers pruning
	mgrTight := NewManager(NewGraph(), &MockStorage{})
	mgrTight.NumCtx = 50 // Extremely tight context

	rootTight, _ := mgrTight.CreateNode("", RoleSystem, "System prompt", false)
	currTightID := rootTight.ID
	for i := 1; i <= 8; i++ {
		user, _ := mgrTight.CreateNode(currTightID, RoleUser, fmt.Sprintf("User question %d with long rambling text to consume tokens", i), false)
		asst, _ := mgrTight.CreateAssistantNode(user.ID, fmt.Sprintf("Answer %d", i), fmt.Sprintf("Reasoning thought for turn %d with long text", i), nil, false)
		currTightID = asst.ID
	}

	messagesTight, err := mgrTight.BuildLLMContext(currTightID, false)
	if err != nil {
		t.Fatalf("failed to build tight LLM context: %v", err)
	}

	tightThoughtsRetained := 0
	for _, msg := range messagesTight {
		if msg.Role == RoleAssistant && msg.Thought != "" {
			tightThoughtsRetained++
		}
	}

	// Under extreme pressure, distant thoughts must be pruned, keeping only the active/recent grace turns
	if tightThoughtsRetained >= 8 {
		t.Errorf("expected distant thoughts to be pruned under tight context pressure, but got %d", tightThoughtsRetained)
	}
}

func TestCompactRangeWithDirective_TrajectoryAndSteering(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})

	root, _ := mgr.CreateNode("", RoleSystem, "You are George 🦉📚", false)
	user1, _ := mgr.CreateNode(root.ID, RoleUser, "Refactor the database schema", false)
	asst1, _ := mgr.CreateAssistantNode(user1.ID, "Schema updated 🛠️💻", "Thinking...", nil, false)
	user2, _ := mgr.CreateNode(asst1.ID, RoleUser, "Verify the queries", false)
	asst2, _ := mgr.CreateAssistantNode(user2.ID, "Queries verified 🔍📁", "Analyzing...", nil, false)

	mockProvider := &MockLLMProvider{
		ResponseContent: "Milestone: Database schema refactored and verified.",
	}

	superNode, err := mgr.CompactRangeWithDirective(
		context.Background(),
		mockProvider,
		[]string{user1.ID, asst1.ID, user2.ID, asst2.ID},
		"focus on database performance",
	)
	if err != nil {
		t.Fatalf("failed to compact range: %v", err)
	}

	// 1. Verify supernode content contains trajectory banner
	if !strings.Contains(superNode.Content, "🎯 Trajectory: 🛠️💻 ➔ 🔍📁") {
		t.Errorf("expected supernode content to contain trajectory header, got: %s", superNode.Content)
	}
	if !strings.Contains(superNode.Content, "Milestone: Database schema refactored and verified.") {
		t.Errorf("expected supernode content to contain summary text, got: %s", superNode.Content)
	}

	// 2. Verify supernode parentage is root
	if superNode.ParentID != root.ID {
		t.Errorf("expected supernode ParentID to be root (%s), got: %s", root.ID, superNode.ParentID)
	}
}

func TestManager_AutonomousVectorLoop_Mock(t *testing.T) {
	mgr := NewManager(NewGraph(), &MockStorage{})
	mgr.NumCtx = 131072

	// 1. Establish George the Archivist Persona
	root, _ := mgr.CreateNode("", RoleSystem, "You are George the Archivist 🦉📚. Chronicle the workspace.", false)

	// 2. Mission Primer
	primer, _ := mgr.CreateNode(root.ID, RoleUser, "Explore the workspace in 5 turns. Your continuation impulse is 'Please proceed.'", false)
	currID := primer.ID

	// 3. Autonomous 5-turn Vector Loop
	steps := []struct {
		file    string
		signat  string
		thought string
	}{
		{"README.md", "🔍📜", "Analyzing project architecture overview..."},
		{"storage.go", "🛠️💻", "Inspecting SQLite WAL schema and timestamp parsing..."},
		{"server.go", "🔒🛡️", "Verifying PKI certificate generation and SSE event bus..."},
		{"service.go", "🧠📐", "Deducing dynamic context resonance capacity zones..."},
		{"map.go", "🎨✨", "Synthesizing full visual telemetry and TUI tree map..."},
	}

	var turnIDs []string
	turnIDs = append(turnIDs, primer.ID)

	for i, step := range steps {
		userTurn, _ := mgr.CreateNode(currID, RoleUser, "Please proceed.", false)
		turnIDs = append(turnIDs, userTurn.ID)

		tcs := []ToolCall{
			{
				ID:   fmt.Sprintf("call_%d", i+1),
				Type: "function",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      "read_file",
					Arguments: json.RawMessage(fmt.Sprintf(`{"path": "%s"}`, step.file)),
				},
			},
		}

		asstContent := fmt.Sprintf("Inspected %s and recorded observations. %s", step.file, step.signat)
		asstTurn, err := mgr.CreateAssistantNode(userTurn.ID, asstContent, step.thought, tcs, false)
		if err != nil {
			t.Fatalf("failed to create assistant node on step %d: %v", i+1, err)
		}
		_ = mgr.UpdateAssistantObservations(asstTurn.ID, fmt.Sprintf("call_%d", i+1), fmt.Sprintf("Content of %s file...", step.file))

		turnIDs = append(turnIDs, asstTurn.ID)
		currID = asstTurn.ID
	}

	// 4. Verify Context Retention Under 128k Headroom
	messages, err := mgr.BuildLLMContext(currID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	asstCount := 0
	thoughtsRetained := 0
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			asstCount++
			if msg.Thought != "" {
				thoughtsRetained++
			}
		}
	}

	if asstCount != 5 {
		t.Errorf("expected 5 assistant turns, got %d", asstCount)
	}
	if thoughtsRetained != 5 {
		t.Errorf("expected all 5 assistant turns to retain thoughts, got %d", thoughtsRetained)
	}

	// 5. Synthesize 5-turn Milestone Compaction
	mockProvider := &MockLLMProvider{
		ResponseContent: "Milestone: George completed a 5-step exploration of README, storage, server, service, and map.",
	}

	superNode, err := mgr.CompactRangeWithDirective(context.Background(), mockProvider, turnIDs, "synthesize architectural chronicles")
	if err != nil {
		t.Fatalf("failed to compact 5-turn vector: %v", err)
	}

	expectedTrajectory := "🎯 Trajectory: 🔍📜 ➔ 🛠️💻 ➔ 🔒🛡️ ➔ 🧠📐 ➔ 🎨✨"
	if !strings.Contains(superNode.Content, expectedTrajectory) {
		t.Errorf("expected supernode to contain trajectory header %q, got:\n%s", expectedTrajectory, superNode.Content)
	}
	if superNode.ParentID != root.ID {
		t.Errorf("expected supernode to attach to root (%s), got %s", root.ID, superNode.ParentID)
	}
}
