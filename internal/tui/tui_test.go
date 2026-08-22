package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/server"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestUpdateStateTransitions(t *testing.T) {
	// 1. Setup dependencies
	tmpDir, err := os.MkdirTemp("", "please-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.jsonl")

	storage := engine.NewJSONLStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Hello world",
	}

	// 2. Initialize Model
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// Ensure we start in SetupMode if graph is empty
	if !m.SetupMode {
		t.Error("Expected model to start in SetupMode for empty graph")
	}

	// 3. Simulate System Prompt input
	m.TextInput.SetValue("You are a helpful assistant")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.SetupMode {
		t.Error("Expected model to exit SetupMode after system prompt")
	}
	if m.CurrentID == "" {
		t.Error("Expected CurrentID to be set after system prompt")
	}

	// 4. Simulate User Message input
	m.TextInput.SetValue("Hi there")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.IsThinking {
		t.Error("Expected model to be in IsThinking state after user input")
	}

	// 5. Simulate LLM Stream Message
	m, _ = updateModel(m, llmStreamMsg{content: "Hello", parentID: m.CurrentID})

	if !m.IsThinking {
		t.Error("Expected IsThinking to be true during streaming")
	}
	if m.CurrentStreamingContent != "Hello" {
		t.Errorf("Expected CurrentStreamingContent to be 'Hello', got '%s'", m.CurrentStreamingContent)
	}

	// 6. Simulate LLM Stream Finished
	m, _ = updateModel(m, llmStreamFinishedMsg{parentID: m.CurrentID})

	if m.IsThinking {
		t.Error("Expected IsThinking to be false after stream finished")
	}
	if m.CurrentStreamingContent != "" {
		t.Error("Expected CurrentStreamingContent to be cleared after finish")
	}
}

func TestThoughtStreaming(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "please-test-thought")
	defer os.RemoveAll(tmpDir)
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Setup - Create root node
	m.TextInput.SetValue("System prompt")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	// 2. User Message
	m.TextInput.SetValue("Hello")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	userID := m.CurrentID

	// 3. Thought stream
	m, _ = updateModel(m, llmThoughtStreamMsg{thought: "Thinking...", parentID: userID})
	if m.CurrentStreamingThought != "Thinking..." {
		t.Errorf("Expected streaming thought 'Thinking...', got '%s'", m.CurrentStreamingThought)
	}

	// 4. Content stream
	m, _ = updateModel(m, llmStreamMsg{content: "Hi!", parentID: userID})
	if m.CurrentStreamingContent != "Hi!" {
		t.Errorf("Expected streaming content 'Hi!', got '%s'", m.CurrentStreamingContent)
	}

	// 5. Finish stream
	m, _ = updateModel(m, llmStreamFinishedMsg{parentID: userID})

	// 6. Verify persistence in the Assistant Node
	// The path should now be: User -> Assistant (containing Thought)
	finalNode, _ := m.Manager.GetNode(m.CurrentID)

	if finalNode.Thought != "Thinking..." {
		t.Errorf("Expected saved thought 'Thinking...', got '%s'", finalNode.Thought)
	}

	if finalNode.Content != "Hi!" {
		t.Errorf("Expected saved content 'Hi!', got '%s'", finalNode.Content)
	}
}

func TestHandleCommand(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	storage := engine.NewJSONLStorage(":memory:", "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	tests := []struct {
		input    string
		handled  bool
		expected string // Expected notification or state change hint
	}{
		{"hello", false, ""},
		{"/unknown", true, "Unknown command: /unknown"},
		{"/persona", true, ""}, // Persona setup mode triggered
		{"/audit", true, "Audit Mode enabled: Internal nodes visible."},
	}

	for _, tt := range tests {
		newM, _, handled := m.HandleCommand(tt.input)
		if handled != tt.handled {
			t.Errorf("input %s: expected handled %v, got %v", tt.input, tt.handled, handled)
		}

		resModel := newM.(*Model)
		if tt.handled && tt.expected != "" && resModel.Notification != tt.expected {
			t.Errorf("input %s: expected notification %s, got %s", tt.input, tt.expected, resModel.Notification)
		}

		if tt.input == "/persona" && !resModel.PersonaSetupMode {
			t.Errorf("input %s: expected PersonaSetupMode to be true", tt.input)
		}

		if tt.input == "/audit" && !resModel.AuditMode {
			t.Errorf("input %s: expected AuditMode to be true", tt.input)
		}
	}
}

// Helper to handle the (tea.Model, tea.Cmd) return from Update
func updateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	// Our Update returns (tea.Model, tea.Cmd), but we want the concrete Model
	newModel, cmd := m.Update(msg)
	return *newModel.(*Model), cmd
}

func TestNaturalPacing(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "please-test-pacing")
	defer os.RemoveAll(tmpDir)
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := true
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Exit setup mode
	m.TextInput.SetValue("System prompt")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	// 2. User input
	m.TextInput.SetValue("Paced request")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	userID := m.CurrentID

	// 3. Stream paced chunk: "A. B"
	m, _ = updateModel(m, llmStreamMsg{content: "A. B", parentID: userID})

	if m.CurrentStreamingContent != "" {
		t.Errorf("Expected CurrentStreamingContent to be empty initially due to pacing, got '%s'", m.CurrentStreamingContent)
	}
	if !m.PacingActive {
		t.Error("Expected PacingActive to be true")
	}
	if string(m.PacingBuffer) != "A. B" {
		t.Errorf("Expected PacingBuffer to contain 'A. B', got '%s'", string(m.PacingBuffer))
	}

	// 4. Pop first character ('A')
	m, _ = updateModel(m, pacingTickMsg{})
	if m.CurrentStreamingContent != "A" {
		t.Errorf("Expected CurrentStreamingContent to be 'A', got '%s'", m.CurrentStreamingContent)
	}

	// 5. Pop second character ('.')
	m, _ = updateModel(m, pacingTickMsg{})
	if m.CurrentStreamingContent != "A." {
		t.Errorf("Expected CurrentStreamingContent to be 'A.', got '%s'", m.CurrentStreamingContent)
	}

	// 6. Test Skip pacing mid-stream
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.CurrentStreamingContent != "A. B" {
		t.Errorf("Expected CurrentStreamingContent to be fully flushed to 'A. B', got '%s'", m.CurrentStreamingContent)
	}
	if m.PacingActive {
		t.Error("Expected PacingActive to be false after skip")
	}
}

func TestToolExecutionErrorRetention(t *testing.T) {
	// 1. Setup dependencies
	tmpDir, err := os.MkdirTemp("", "please-test-tool-err")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, err := engine.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatal(err)
	}
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}

	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// Register a tool that fails
	m.Manager.Registry.Register(engine.Tool{
		Name:        "fail_tool",
		Description: "A tool that returns an error and some output",
		Parameters:  nil,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "some partial output here", fmt.Errorf("something went wrong")
		},
	})

	// Create an assistant node with pending tool call
	assistantNode, err := m.Manager.CreateAssistantNode("", "calling tool", "", []engine.ToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      "fail_tool",
				Arguments: json.RawMessage(`{}`),
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	m.CurrentID = assistantNode.ID
	m.InterleavingNodeID = assistantNode.ID
	m.PendingToolCalls = assistantNode.ToolCalls

	// Trigger execution command
	cmd := m.executeToolsCmd()
	msg := cmd() // Run synchronously

	resMsg, ok := msg.(toolsExecutedMsg)
	if !ok {
		t.Fatalf("expected toolsExecutedMsg, got %T", msg)
	}
	if resMsg.err != nil {
		t.Fatalf("unexpected error inside toolsExecutedMsg: %v", resMsg.err)
	}

	// Verify observations in storage / memory
	node, err := m.Manager.GetNode(assistantNode.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(node.Observations) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(node.Observations))
	}

	obs := node.Observations[0]
	if obs.ToolCallID != "call_123" {
		t.Errorf("expected ToolCallID 'call_123', got '%s'", obs.ToolCallID)
	}

	expectedResult := "Error: something went wrong\nOutput:\nsome partial output here"
	if obs.Result != expectedResult {
		t.Errorf("expected observation result:\n%s\ngot:\n%s", expectedResult, obs.Result)
	}
}

func TestConfigCommand_RunnerOptions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	ollamaProvider := engine.NewOllamaProvider("http://localhost:11434/api/chat", "gemma", nil)
	m := NewModel(cfg, graph, storage, ollamaProvider, "")

	// 1. Set Temperature
	_, _, handled := m.HandleCommand("/config temp 0.75")
	if !handled {
		t.Fatal("expected /config temp to be handled")
	}
	if m.Config.Server == nil || m.Config.Server.Options == nil || m.Config.Server.Options.Temperature == nil || *m.Config.Server.Options.Temperature != 0.75 {
		t.Errorf("expected temperature to be 0.75, got %v", m.Config.Server)
	}
	if ollamaProvider.Options == nil || ollamaProvider.Options.Temperature == nil || *ollamaProvider.Options.Temperature != 0.75 {
		t.Errorf("expected provider options temperature to be 0.75, got %v", ollamaProvider.Options.Temperature)
	}

	// 2. Set Top-P
	m.HandleCommand("/config top_p 0.95")
	if m.Config.Server.Options.TopP == nil || *m.Config.Server.Options.TopP != 0.95 {
		t.Errorf("expected top_p to be 0.95, got %v", m.Config.Server.Options.TopP)
	}

	// 3. Set Top-K
	m.HandleCommand("/config top_k 50")
	if m.Config.Server.Options.TopK == nil || *m.Config.Server.Options.TopK != 50 {
		t.Errorf("expected top_k to be 50, got %v", m.Config.Server.Options.TopK)
	}

	// 4. Set Context Size
	m.HandleCommand("/config ctx 32768")
	if m.Config.Server.Options.NumCtx == nil || *m.Config.Server.Options.NumCtx != 32768 {
		t.Errorf("expected num_ctx to be 32768, got %v", m.Config.Server.Options.NumCtx)
	}

	// 5. Set Max Tokens
	m.HandleCommand("/config max_tokens 4096")
	if m.Config.Server.Options.MaxTokens == nil || *m.Config.Server.Options.MaxTokens != 4096 {
		t.Errorf("expected max_tokens to be 4096, got %v", m.Config.Server.Options.MaxTokens)
	}

	// 6. Reset Temperature
	m.HandleCommand("/config temp default")
	if m.Config.Server.Options.Temperature != nil {
		t.Errorf("expected temperature to be reset to nil, got %v", m.Config.Server.Options.Temperature)
	}

	// 7. Test display output
	m.HandleCommand("/config")
	if m.ViewportOverride == "" {
		t.Fatal("expected /config with no args to set ViewportOverride")
	}

	// Verify that the saved config file was written to tmpDir and NOT user's real config directory
	savedData, err := os.ReadFile(filepath.Join(tmpDir, "config.json"))
	if err != nil {
		t.Fatalf("expected config.json to be saved in tmpDir: %v", err)
	}
	var loaded engine.Config
	if err := json.Unmarshal(savedData, &loaded); err != nil {
		t.Fatalf("failed to unmarshal saved config: %v", err)
	}
	if loaded.Server == nil || loaded.Server.Options == nil || loaded.Server.Options.TopP == nil || *loaded.Server.Options.TopP != 0.95 {
		t.Errorf("expected saved top_p to be 0.95, got %v", loaded.Server)
	}
}

func TestViewportOverrideTextInputAndLiveUpdate(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "please-test-override")
	defer os.RemoveAll(tmpDir)
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{ResponseContent: "Response"}
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Initial setup
	m.TextInput.SetValue("System prompt")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	// 2. Open /config view
	m.TextInput.SetValue("/config")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.ViewportOverride == "" {
		t.Fatal("expected /config to set ViewportOverride")
	}

	// 3. Type while ViewportOverride is active
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.TextInput.Value() != "hi" {
		t.Fatalf("expected TextInput to receive typing during ViewportOverride, got %q", m.TextInput.Value())
	}
	m.TextInput.Reset()

	// 4. Run /config temp 0.85 while config view is open
	m.TextInput.SetValue("/config temp 0.85")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.Config.Server.Options == nil || m.Config.Server.Options.Temperature == nil || *m.Config.Server.Options.Temperature != 0.85 {
		t.Fatalf("expected temperature to be 0.85, got %v", m.Config.Server.Options)
	}
	if !strings.Contains(m.ViewportOverride, "0.85") {
		t.Errorf("expected ViewportOverride to live-update with new temperature, got:\n%s", m.ViewportOverride)
	}

	// 5. Send a regular chat message while ViewportOverride is active
	m.TextInput.SetValue("Let's resume chat")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.ViewportOverride != "" {
		t.Errorf("expected ViewportOverride to be cleared on user message, got %q", m.ViewportOverride)
	}
	if !m.IsThinking {
		t.Error("expected IsThinking to be true after user message")
	}

	// Finish stream
	m, _ = updateModel(m, llmStreamFinishedMsg{parentID: m.CurrentID})

	// 6. Open /help view and test ESC dismiss
	m.TextInput.SetValue("/help")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.ViewportOverride == "" {
		t.Fatal("expected /help to set ViewportOverride")
	}

	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.ViewportOverride != "" {
		t.Errorf("expected ESC to clear ViewportOverride, got %q", m.ViewportOverride)
	}
}

func TestConfigCommand_Workspace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Initial workspace display
	m.HandleCommand("/config")
	if !strings.Contains(m.ViewportOverride, "Workspace:       (current directory)") {
		t.Errorf("expected initial workspace to be (current directory), got:\n%s", m.ViewportOverride)
	}

	// 2. Set workspace to custom directory
	customWs := t.TempDir()
	m.HandleCommand("/config workspace " + customWs)
	if m.Config.Server.WorkspaceDir != customWs {
		t.Errorf("expected WorkspaceDir to be %s, got %s", customWs, m.Config.Server.WorkspaceDir)
	}

	// Check updated /config view
	if !strings.Contains(m.ViewportOverride, customWs) {
		t.Errorf("expected ViewportOverride to show %s, got:\n%s", customWs, m.ViewportOverride)
	}

	// 3. Reset workspace to default
	m.HandleCommand("/config workspace default")
	if m.Config.Server.WorkspaceDir != "" {
		t.Errorf("expected WorkspaceDir to be empty after reset, got %s", m.Config.Server.WorkspaceDir)
	}
	if !strings.Contains(m.ViewportOverride, "Workspace:       (current directory)") {
		t.Errorf("expected ViewportOverride to show (current directory), got:\n%s", m.ViewportOverride)
	}
}

func TestConfigCommand_EncryptionKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Initial display shows disabled
	m.HandleCommand("/config")
	if !strings.Contains(m.ViewportOverride, "Encryption:      (disabled)") {
		t.Errorf("expected initial encryption to be (disabled), got:\n%s", m.ViewportOverride)
	}

	// 2. Set encryption key
	secretKey := "super-secret-key-12345"
	m.HandleCommand("/config key " + secretKey)
	if m.Config.Server.EncryptionKey != secretKey {
		t.Errorf("expected EncryptionKey to be %s, got %s", secretKey, m.Config.Server.EncryptionKey)
	}

	// 3. Verify /config displays redacted key and NEVER reveals plaintext
	if strings.Contains(m.ViewportOverride, secretKey) {
		t.Fatalf("security violation: plaintext encryption key leaked in /config sheet display")
	}
	if !strings.Contains(m.ViewportOverride, "Encryption:      •••••••• (configured)") {
		t.Errorf("expected redacted encryption display, got:\n%s", m.ViewportOverride)
	}

	// 4. Reset encryption key
	m.HandleCommand("/config key default")
	if m.Config.Server.EncryptionKey != "" {
		t.Errorf("expected EncryptionKey to be empty after reset, got %s", m.Config.Server.EncryptionKey)
	}
	if !strings.Contains(m.ViewportOverride, "Encryption:      (disabled)") {
		t.Errorf("expected disabled encryption display after reset, got:\n%s", m.ViewportOverride)
	}
}

func TestNavigateToNode_AssistantAndUserTurns(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Mock assistant reply",
	}
	pacing := false
	cfg := &engine.Config{Client: &engine.ClientConfig{NaturalPacing: &pacing}}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Build conversation: Root System -> User1 -> Assistant1 -> User2 -> Assistant2
	sysNode, _ := m.Manager.CreateNode("", engine.RoleSystem, "System prompt", false)
	m.CurrentID = sysNode.ID

	user1, _ := m.Manager.CreateNode(sysNode.ID, engine.RoleUser, "First question from user", false)
	asst1, _ := m.Manager.CreateNode(user1.ID, engine.RoleAssistant, "First answer from assistant", false)
	user2, _ := m.Manager.CreateNode(asst1.ID, engine.RoleUser, "Second question from user with detail", false)
	user2.Images = []string{"/tmp/test.png"}
	_ = m.Manager.Storage.SaveNode(user2)
	asst2, _ := m.Manager.CreateNode(user2.ID, engine.RoleAssistant, "Second answer from assistant", false)
	m.CurrentID = asst2.ID
	m.SetupMode = false
	m.updateViewportContent()

	// 2. Test navigating to an Assistant Turn via /jump
	m.HandleCommand("/jump " + asst1.ID)
	if m.CurrentID != asst1.ID {
		t.Fatalf("expected CurrentID to be asst1 %s, got %s", asst1.ID, m.CurrentID)
	}
	if m.TextInput.Value() != "" {
		t.Fatalf("expected TextInput to be empty when jumping to assistant turn, got %q", m.TextInput.Value())
	}
	if len(m.PendingImages) != 0 {
		t.Fatalf("expected PendingImages to be empty, got %v", m.PendingImages)
	}

	// 3. Test navigating to a User Turn via /jump (Rewind & Edit)
	m.HandleCommand("/jump " + user2.ID)
	if m.CurrentID != asst1.ID {
		t.Fatalf("expected CurrentID to rewind to user2's parent (asst1 %s), got %s", asst1.ID, m.CurrentID)
	}
	if m.TextInput.Value() != "Second question from user with detail" {
		t.Fatalf("expected TextInput to contain user2 content, got %q", m.TextInput.Value())
	}
	if len(m.PendingImages) != 1 || m.PendingImages[0] != "/tmp/test.png" {
		t.Fatalf("expected PendingImages to restore user2 images, got %v", m.PendingImages)
	}
	if !strings.Contains(m.Notification, "Rewound") {
		t.Errorf("expected notification about rewinding, got %q", m.Notification)
	}
	// Verify chat history rendered only up to asst1 (user2 should not be in active chat buffer)
	if strings.Contains(m.ChatHistoryBuffer, "Second question from user with detail") {
		t.Errorf("expected chat history buffer to exclude rewound user2 prompt, got:\n%s", m.ChatHistoryBuffer)
	}
	if !strings.Contains(m.ChatHistoryBuffer, "First answer from assistant") {
		t.Errorf("expected chat history buffer to include asst1, got:\n%s", m.ChatHistoryBuffer)
	}

	// 4. Test submitting modified turn creates a new branch off asst1
	m.TextInput.SetValue("Modified second question branching off asst1")
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Process stream finish
	m, _ = updateModel(m, llmStreamFinishedMsg{parentID: m.CurrentID})

	// Assert the new leaf's lineage leads back to asst1
	newPath, err := m.Manager.GetPath(m.CurrentID)
	if err != nil || len(newPath) < 4 {
		t.Fatalf("unexpected new path: %v, err: %v", newPath, err)
	}
	// newPath should be: [sysNode, user1, asst1, newUserBranch, newAsstBranch]
	if newPath[2].ID != asst1.ID {
		t.Errorf("expected branch ancestor to be asst1 %s, got %s", asst1.ID, newPath[2].ID)
	}
	if newPath[3].Content != "Modified second question branching off asst1" {
		t.Errorf("expected new user branch content, got %s", newPath[3].Content)
	}

	// 5. Test Map Mode Enter key on a user turn
	m.HandleCommand("/map")
	if m.ViewMode != ModeMap {
		t.Fatalf("expected ViewMode to be ModeMap, got %v", m.ViewMode)
	}
	// Find index of user1 in MapNodeIDs
	user1Idx := -1
	for i, id := range m.MapNodeIDs {
		if id == user1.ID {
			user1Idx = i
			break
		}
	}
	if user1Idx == -1 {
		t.Fatalf("user1 %s not found in MapNodeIDs: %v", user1.ID, m.MapNodeIDs)
	}
	m.MapSelectionIndex = user1Idx
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.ViewMode != ModeChat {
		t.Errorf("expected ViewMode to return to ModeChat after enter, got %v", m.ViewMode)
	}
	if m.CurrentID != sysNode.ID {
		t.Errorf("expected CurrentID to rewind to user1's parent (sysNode %s), got %s", sysNode.ID, m.CurrentID)
	}
	if m.TextInput.Value() != "First question from user" {
		t.Errorf("expected TextInput to contain user1 content, got %q", m.TextInput.Value())
	}
}

func TestContextStats_DynamicColoring(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "vault.db")

	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mockProvider := &engine.MockLLMProvider{}
	pacing := false
	numCtx := 1000 // Small context limit for testing thresholds
	cfg := &engine.Config{
		Client: &engine.ClientConfig{NaturalPacing: &pacing},
		Server: &engine.ServerConfig{
			Options: &engine.ModelOptions{NumCtx: &numCtx},
		},
	}
	m := NewModel(cfg, graph, storage, mockProvider, "")

	// 1. Initial empty state: < 60% -> Emerald green
	used, limit, pct, color := m.ContextStats()
	if limit != 1000 {
		t.Errorf("expected limit 1000, got %d", limit)
	}
	if pct >= 60 {
		t.Errorf("expected low initial pct, got %d", pct)
	}
	if color != lipgloss.Color("#4ade80") {
		t.Errorf("expected emerald green color, got %v", color)
	}

	// 2. Medium state: ~70% -> Amber gold
	m.TextInput.SetValue(strings.Repeat("a", 2600)) // ~684 tokens -> 68%
	used, _, pct, color = m.ContextStats()
	if pct < 60 || pct >= 85 {
		t.Errorf("expected amber pct (60-85), got %d (used: %d)", pct, used)
	}
	if color != lipgloss.Color("#facc15") {
		t.Errorf("expected amber color, got %v", color)
	}

	// 3. High state: > 85% -> Rose coral
	m.TextInput.SetValue(strings.Repeat("a", 3600)) // ~947 tokens -> 94%
	_, _, pct, color = m.ContextStats()
	if pct < 85 {
		t.Errorf("expected coral pct (>85), got %d", pct)
	}
	if color != lipgloss.Color("#fb7185") {
		t.Errorf("expected coral color, got %v", color)
	}
}

func TestRemoteDaemonEvents_LiveSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)

	// Create root node in storage
	rootNode, err := mgr.CreateNode("", engine.RoleSystem, "You are a helpful assistant.", false)
	if err != nil {
		t.Fatalf("failed to create root node: %v", err)
	}

	cfg := engine.NewDefaultConfig()
	m := NewModel(cfg, graph, storage, nil, rootNode.ID)
	m.RemoteURL = "http://127.0.0.1:8443"

	// Simulate receiving a node_saved event from Terminal 1
	newNode, err := mgr.CreateNode(rootNode.ID, engine.RoleUser, "Message from Terminal 1", false)
	if err != nil {
		t.Fatalf("failed to create new node: %v", err)
	}

	eventMsg := remoteDaemonEventMsg{
		Event: server.DaemonEvent{
			Type: server.EventNodeSaved,
			Payload: map[string]interface{}{
				"node_id":   newNode.ID,
				"parent_id": rootNode.ID,
				"role":      "user",
			},
		},
	}

	updatedModel, _ := m.handleRemoteDaemonEvent(eventMsg)
	updatedM := updatedModel.(*Model)

	if updatedM.CurrentID != newNode.ID {
		t.Errorf("expected CurrentID to advance to %s, got %s", newNode.ID, updatedM.CurrentID)
	}
}

func TestThoughtFolding_TabAndShiftTab(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)

	rootNode, _ := mgr.CreateNode("", engine.RoleSystem, "System prompt", false)
	userNode, _ := mgr.CreateNode(rootNode.ID, engine.RoleUser, "User prompt", false)
	asstNode, _ := mgr.CreateAssistantNode(userNode.ID, "Assistant response", "This is an internal reasoning thought process.", nil, false)

	cfg := engine.NewDefaultConfig()
	m := NewModel(cfg, graph, storage, nil, asstNode.ID)
	m.Width = 80

	// 1. Initial State: Folded by default (DefaultFoldThoughts = true)
	rendered := m.renderNode(asstNode)
	if !strings.Contains(rendered, "▶ Thought Process") {
		t.Errorf("expected folded badge '▶ Thought Process', got:\n%s", rendered)
	}
	if strings.Contains(rendered, "This is an internal reasoning thought process.") {
		t.Errorf("expected thought text to be hidden when folded, got:\n%s", rendered)
	}

	// 2. Press Tab: Expands the active node
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	resM, _, handled := m.handleChatKeys(tabMsg)
	if !handled {
		t.Errorf("expected Tab to be handled in ModeChat")
	}
	m = *resM

	renderedExpanded := m.renderNode(asstNode)
	if !strings.Contains(renderedExpanded, "▼ Thought Process") {
		t.Errorf("expected expanded badge '▼ Thought Process', got:\n%s", renderedExpanded)
	}
	if !strings.Contains(renderedExpanded, "This is an internal reasoning thought process.") {
		t.Errorf("expected thought text to be visible when expanded, got:\n%s", renderedExpanded)
	}

	// 3. Press Tab again: Folds back
	resM, _, _ = m.handleChatKeys(tabMsg)
	m = *resM
	renderedFoldedAgain := m.renderNode(asstNode)
	if !strings.Contains(renderedFoldedAgain, "▶ Thought Process") {
		t.Errorf("expected folded badge after 2nd Tab, got:\n%s", renderedFoldedAgain)
	}

	// 4. Test /fold command
	foldCmd := &FoldCommand{}
	foldModel, _ := foldCmd.Execute(&m, nil)
	m = *foldModel.(*Model)
	if !m.isThoughtExpanded(asstNode.ID) {
		t.Errorf("expected /fold to expand thought")
	}

	// 5. Test /fold all
	foldAllModel, _ := foldCmd.Execute(&m, []string{"all"})
	m = *foldAllModel.(*Model)
	if m.isThoughtExpanded(asstNode.ID) {
		t.Errorf("expected /fold all to collapse when all were expanded")
	}
}

func TestMapMode_LiveSync_SelectionPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)

	rootNode, _ := mgr.CreateNode("", engine.RoleSystem, "System prompt", false)
	user1, _ := mgr.CreateNode(rootNode.ID, engine.RoleUser, "Turn 1", false)
	_, _ = mgr.CreateNode(rootNode.ID, engine.RoleUser, "Turn 2", false)

	cfg := engine.NewDefaultConfig()
	m := NewModel(cfg, graph, storage, nil, rootNode.ID)
	m.ViewMode = ModeMap
	m.syncMapSelection()

	// Select user1 in the map
	var user1Index int = -1
	for i, id := range m.MapNodeIDs {
		if id == user1.ID {
			user1Index = i
			break
		}
	}
	if user1Index == -1 {
		t.Fatalf("expected user1 to be in MapNodeIDs")
	}
	m.MapSelectionIndex = user1Index

	// Simulate receiving EventNodeSaved from remote daemon for a new turn under root
	newNode, _ := mgr.CreateNode(rootNode.ID, engine.RoleUser, "Turn 3 (New)", false)
	eventMsg := remoteDaemonEventMsg{
		Event: server.DaemonEvent{
			Type: server.EventNodeSaved,
			Payload: map[string]interface{}{
				"node_id":   newNode.ID,
				"parent_id": rootNode.ID,
				"role":      "user",
			},
		},
	}

	updatedModel, _ := m.handleRemoteDaemonEvent(eventMsg)
	updatedM := updatedModel.(*Model)

	// Verify that the currently highlighted node remains user1.ID
	selectedNodeID := updatedM.MapNodeIDs[updatedM.MapSelectionIndex]
	if selectedNodeID != user1.ID {
		t.Errorf("expected selected node to remain user1 (%s), but got %s (index: %d)", user1.ID, selectedNodeID, updatedM.MapSelectionIndex)
	}
}

func TestMapMode_LiveSync_PrunedSelectionFallback(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)

	rootNode, _ := mgr.CreateNode("", engine.RoleSystem, "System prompt", false)
	userNode, _ := mgr.CreateNode(rootNode.ID, engine.RoleUser, "To be pruned", false)

	cfg := engine.NewDefaultConfig()
	m := NewModel(cfg, graph, storage, nil, userNode.ID)
	m.ViewMode = ModeMap
	m.syncMapSelection()

	// Highlight userNode
	for i, id := range m.MapNodeIDs {
		if id == userNode.ID {
			m.MapSelectionIndex = i
			break
		}
	}

	// Prune userNode
	_ = mgr.PruneBranch(userNode.ID)

	eventMsg := remoteDaemonEventMsg{
		Event: server.DaemonEvent{
			Type: server.EventBranchPruned,
			Payload: map[string]interface{}{
				"root_id": userNode.ID,
			},
		},
	}

	updatedModel, _ := m.handleRemoteDaemonEvent(eventMsg)
	updatedM := updatedModel.(*Model)

	// Verify MapSelectionIndex clamped safely to rootNode
	if len(updatedM.MapNodeIDs) == 0 {
		t.Fatalf("expected at least rootNode to remain in MapNodeIDs")
	}
	selectedNodeID := updatedM.MapNodeIDs[updatedM.MapSelectionIndex]
	if selectedNodeID != rootNode.ID {
		t.Errorf("expected selection to fall back to rootNode (%s), got %s", rootNode.ID, selectedNodeID)
	}
}
