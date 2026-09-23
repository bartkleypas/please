package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/tools"
	acpsdk "github.com/coder/acp-go-sdk"
)

// mockStorage provides an in-memory storage implementation for tests.
type mockStorage struct {
	mu       sync.Mutex
	sessions map[string]string
	nodes    map[string]*graph.Node
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		sessions: make(map[string]string),
		nodes:    make(map[string]*graph.Node),
	}
}

func (s *mockStorage) SaveNode(node *graph.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = node
	return nil
}

func (s *mockStorage) LoadGraph() (*graph.Graph, string, error) {
	g := graph.NewGraph()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.nodes {
		g.AddNode(n)
	}
	return g, "", nil
}

func (s *mockStorage) GarbageCollect() (int64, error) {
	return 0, nil
}

func (s *mockStorage) UpdateNodeMetadata(node *graph.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = node
	return nil
}

func (s *mockStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	return nil
}

func (s *mockStorage) UpdateNodeObservations(nodeID string, obs []domain.ToolObservation) error {
	return nil
}

func (s *mockStorage) SaveSessionHead(sessionID, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = nodeID
	return nil
}

func (s *mockStorage) GetSessionHead(sessionID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[sessionID], nil
}

func (s *mockStorage) ListSessions() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]string, len(s.sessions))
	for k, v := range s.sessions {
		cp[k] = v
	}
	return cp, nil
}

// mockACPClient implements acpsdk.Client to receive updates and permissions from the agent.
type mockACPClient struct {
	mu            sync.Mutex
	messages      []string
	thoughts      []string
	toolCalls     []acpsdk.SessionUpdateToolCall
	toolUpdates   []acpsdk.SessionToolCallUpdate
	permissionReq *acpsdk.RequestPermissionRequest
	decision      string // "allow_once", "allow_always", "reject_once", "cancel"
}

func (c *mockACPClient) ReadTextFile(ctx context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, nil
}

func (c *mockACPClient) WriteTextFile(ctx context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, nil
}

func (c *mockACPClient) RequestPermission(ctx context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissionReq = &params

	if c.decision == "cancel" {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.NewRequestPermissionOutcomeCancelled(),
		}, nil
	}

	optID := acpsdk.PermissionOptionId(c.decision)
	if optID == "" {
		optID = "allow_once"
	}

	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.NewRequestPermissionOutcomeSelected(optID),
	}, nil
}

func (c *mockACPClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if params.Update.AgentMessageChunk != nil && params.Update.AgentMessageChunk.Content.Text != nil {
		c.messages = append(c.messages, params.Update.AgentMessageChunk.Content.Text.Text)
	}
	if params.Update.AgentThoughtChunk != nil && params.Update.AgentThoughtChunk.Content.Text != nil {
		c.thoughts = append(c.thoughts, params.Update.AgentThoughtChunk.Content.Text.Text)
	}
	if params.Update.ToolCall != nil {
		c.toolCalls = append(c.toolCalls, *params.Update.ToolCall)
	}
	if params.Update.ToolCallUpdate != nil {
		c.toolUpdates = append(c.toolUpdates, *params.Update.ToolCallUpdate)
	}
	return nil
}

func (c *mockACPClient) CreateTerminal(ctx context.Context, params acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, nil
}

func (c *mockACPClient) KillTerminal(ctx context.Context, params acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, nil
}

func (c *mockACPClient) TerminalOutput(ctx context.Context, params acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, nil
}

func (c *mockACPClient) ReleaseTerminal(ctx context.Context, params acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func (c *mockACPClient) WaitForTerminalExit(ctx context.Context, params acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, nil
}

func setupConnectedHarness(t *testing.T, clientDecision string, provider providers.Provider) (*acpsdk.ClientSideConnection, *Agent, *mockACPClient, func()) {
	t.Helper()
	storage := newMockStorage()
	g := graph.NewGraph()
	mgr := engine.NewManager(g, storage)
	mgr.Registry = tools.NewToolRegistry()
	cfg := config.NewDefaultConfig()

	agent := NewAgent(mgr, provider, cfg, "/tmp/test-workspace")
	mockClient := &mockACPClient{decision: clientDecision}

	clientToAgentR, clientToAgentW := io.Pipe()
	agentToClientR, agentToClientW := io.Pipe()

	connAgent := acpsdk.NewAgentSideConnection(agent, agentToClientW, clientToAgentR)
	agent.SetConnection(connAgent)

	connClient := acpsdk.NewClientSideConnection(mockClient, clientToAgentW, agentToClientR)

	cleanup := func() {
		_ = clientToAgentW.Close()
		_ = clientToAgentR.Close()
		_ = agentToClientW.Close()
		_ = agentToClientR.Close()
	}

	return connClient, agent, mockClient, cleanup
}

func TestACPAgent_Initialize(t *testing.T) {
	connClient, _, _, cleanup := setupConnectedHarness(t, "allow_once", nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := connClient.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if resp.ProtocolVersion != acpsdk.ProtocolVersionNumber {
		t.Errorf("expected protocol version %d, got %d", acpsdk.ProtocolVersionNumber, resp.ProtocolVersion)
	}
	if resp.AgentInfo == nil || resp.AgentInfo.Name != "please" {
		t.Errorf("expected agent name 'please', got %v", resp.AgentInfo)
	}
	if resp.AgentCapabilities.SessionCapabilities.Close == nil {
		t.Errorf("expected close capability advertised")
	}
	if resp.AgentCapabilities.SessionCapabilities.List == nil {
		t.Errorf("expected list capability advertised")
	}
	if resp.AgentCapabilities.SessionCapabilities.Resume == nil {
		t.Errorf("expected resume capability advertised")
	}
}

func TestACPAgent_SessionLifecycle(t *testing.T) {
	connClient, _, _, cleanup := setupConnectedHarness(t, "allow_once", nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. NewSession
	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/workspace/demo",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	if newResp.SessionId == "" {
		t.Fatalf("expected non-empty sessionId")
	}
	if newResp.Modes == nil || len(newResp.Modes.AvailableModes) != 3 {
		t.Fatalf("expected 3 available modes, got %v", newResp.Modes)
	}
	if newResp.Modes.CurrentModeId != "standard" {
		t.Errorf("expected default mode 'standard', got %q", newResp.Modes.CurrentModeId)
	}

	// 2. SetSessionMode
	_, err = connClient.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: newResp.SessionId,
		ModeId:    "permissive",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	// 3. ResumeSession
	resumeResp, err := connClient.ResumeSession(ctx, acpsdk.ResumeSessionRequest{
		SessionId: newResp.SessionId,
		Cwd:       "/workspace/demo",
	})
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}
	if resumeResp.Modes == nil || resumeResp.Modes.CurrentModeId != "permissive" {
		t.Errorf("expected resumed mode 'permissive', got %v", resumeResp.Modes)
	}

	// 4. CloseSession
	_, err = connClient.CloseSession(ctx, acpsdk.CloseSessionRequest{
		SessionId: newResp.SessionId,
	})
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}
}

func TestACPAgent_PromptStreaming(t *testing.T) {
	mockProv := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, tools []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			return "Hello from please ACP agent!", "Reasoning about user request", nil, nil
		},
	}

	connClient, _, mockClient, cleanup := setupConnectedHarness(t, "allow_once", mockProv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/workspace/demo",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	msgID := "msg-12345"
	promptResp, err := connClient.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: newResp.SessionId,
		MessageId: &msgID,
		Prompt: []acpsdk.ContentBlock{
			acpsdk.TextBlock("Hello please!"),
		},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if promptResp.StopReason != acpsdk.StopReasonEndTurn {
		t.Errorf("expected StopReason 'end_turn', got %q", promptResp.StopReason)
	}
	if promptResp.UserMessageId == nil || *promptResp.UserMessageId != msgID {
		t.Errorf("expected echoed userMessageId %q, got %v", msgID, promptResp.UserMessageId)
	}

	mockClient.mu.Lock()
	defer mockClient.mu.Unlock()

	joinedMsg := strings.Join(mockClient.messages, "")
	if !strings.Contains(joinedMsg, "Hello from please ACP agent!") {
		t.Errorf("expected streamed agent message chunk, got: %q", joinedMsg)
	}

	joinedThought := strings.Join(mockClient.thoughts, "")
	if !strings.Contains(joinedThought, "Reasoning about user request") {
		t.Errorf("expected streamed thought chunk, got: %q", joinedThought)
	}
}

func TestACPAgent_PermissionGate_AllowOnce(t *testing.T) {
	toolExecuted := false
	mockProv := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, tools []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			if !toolExecuted {
				return "", "", []domain.ToolCall{
					{
						ID:   "call_cmd_1",
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "execute_cmd",
							Arguments: json.RawMessage(`{"cmd":"echo hi"}`),
						},
					},
				}, nil
			}
			return "Command succeeded", "", nil, nil
		},
	}

	connClient, agent, mockClient, cleanup := setupConnectedHarness(t, "allow_once", mockProv)
	defer cleanup()

	agent.mgr.Registry.Register(tools.Tool{
		Name:        "execute_cmd",
		Category:    domain.CategoryExecute,
		Interactive: true,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			toolExecuted = true
			return "hi\n", nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Switch to permissive mode so execute tools are available
	_, _ = connClient.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: newResp.SessionId,
		ModeId:    "permissive",
	})

	_, err = connClient.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("Run command")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if !toolExecuted {
		t.Errorf("expected tool to execute after allow_once permission")
	}

	mockClient.mu.Lock()
	defer mockClient.mu.Unlock()

	if mockClient.permissionReq == nil {
		t.Errorf("expected client to receive RequestPermission request")
	} else if mockClient.permissionReq.ToolCall.ToolCallId != "call_cmd_1" {
		t.Errorf("unexpected tool call id: %s", mockClient.permissionReq.ToolCall.ToolCallId)
	}
}

func TestACPAgent_PermissionGate_RejectOnce(t *testing.T) {
	toolExecuted := false
	mockProv := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, tools []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			if len(messages) > 0 {
				last := messages[len(messages)-1]
				if strings.Contains(last.Content, "User denied execution") {
					return "I see the command was denied, stopping.", "", nil, nil
				}
			}
			return "", "", []domain.ToolCall{
				{
					ID:   "call_cmd_2",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "execute_cmd",
						Arguments: json.RawMessage(`{"cmd":"rm -rf /"}`),
					},
				},
			}, nil
		},
	}

	connClient, agent, mockClient, cleanup := setupConnectedHarness(t, "reject_once", mockProv)
	defer cleanup()

	agent.mgr.Registry.Register(tools.Tool{
		Name:        "execute_cmd",
		Category:    domain.CategoryExecute,
		Interactive: true,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			toolExecuted = true
			return "should not run", nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	_, _ = connClient.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: newResp.SessionId,
		ModeId:    "permissive",
	})

	_, err = connClient.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("Delete everything")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if toolExecuted {
		t.Errorf("tool executed despite client rejecting permission!")
	}

	mockClient.mu.Lock()
	defer mockClient.mu.Unlock()

	joinedMsg := strings.Join(mockClient.messages, "")
	if !strings.Contains(joinedMsg, "I see the command was denied, stopping.") {
		t.Errorf("expected model to adapt after denial, got: %q", joinedMsg)
	}
}

func TestACPAgent_PermissionGate_AllowAlways(t *testing.T) {
	execCount := 0
	turn := 1
	step := 0
	mockProv := &providers.MockLLMProvider{
		StreamHandler: func(messages []domain.Message, tools []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			step++
			if step%2 == 1 {
				return "", "", []domain.ToolCall{
					{
						ID:   fmt.Sprintf("call_cmd_%d", step),
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      "safe_cmd",
							Arguments: json.RawMessage(`{}`),
						},
					},
				}, nil
			}
			return fmt.Sprintf("Turn %d done", turn), "", nil, nil
		},
	}

	connClient, agent, mockClient, cleanup := setupConnectedHarness(t, "allow_always", mockProv)
	defer cleanup()

	agent.mgr.Registry.Register(tools.Tool{
		Name:        "safe_cmd",
		Category:    domain.CategoryExecute,
		Interactive: true,
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			execCount++
			return "ok", nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	_, _ = connClient.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: newResp.SessionId,
		ModeId:    "permissive",
	})

	// Turn 1: client responds "allow_always"
	_, err = connClient.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("First run")},
	})
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	if execCount != 1 {
		t.Fatalf("expected 1 execution, got %d", execCount)
	}

	// Change client decision to "reject_once" for subsequent calls
	mockClient.mu.Lock()
	mockClient.decision = "reject_once"
	mockClient.permissionReq = nil
	mockClient.mu.Unlock()

	// Turn 2: since "allow_always" was stored for "safe_cmd", it should bypass client permission and execute!
	turn = 2
	_, err = connClient.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("Second run")},
	})
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	if execCount != 2 {
		t.Fatalf("expected 2 executions due to allow_always bypass, got %d", execCount)
	}

	mockClient.mu.Lock()
	defer mockClient.mu.Unlock()
	if mockClient.permissionReq != nil {
		t.Errorf("expected permission request to be bypassed for Turn 2")
	}
}

func TestACPAgent_ListSessions(t *testing.T) {
	connClient, agent, _, cleanup := setupConnectedHarness(t, "allow_once", nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Add mock node and session head
	node := &graph.Node{
		ID:        "node-test-1",
		Role:      graph.RoleAssistant,
		Content:   "Summary of past session work",
		Timestamp: time.Now(),
	}
	_ = agent.mgr.Storage.SaveNode(node)
	_ = agent.mgr.Storage.SaveSessionHead("test-session-alpha", node.ID)

	listResp, err := connClient.ListSessions(ctx, acpsdk.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	found := false
	for _, s := range listResp.Sessions {
		if s.SessionId == "test-session-alpha" {
			found = true
			if s.Title == nil || !strings.Contains(*s.Title, "Summary of past session work") {
				t.Errorf("unexpected session title: %v", s.Title)
			}
			if s.UpdatedAt == nil {
				t.Errorf("expected non-nil UpdatedAt")
			}
			break
		}
	}

	if !found {
		t.Errorf("expected 'test-session-alpha' in listed sessions")
	}
}

func TestACPAgent_Cancel(t *testing.T) {
	connClient, agent, _, cleanup := setupConnectedHarness(t, "allow_once", nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	newResp, err := connClient.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Register a mock cancel func
	cancelled := false
	agent.sessionCancels.Store(newResp.SessionId, context.CancelFunc(func() {
		cancelled = true
	}))

	err = connClient.Cancel(ctx, acpsdk.CancelNotification{
		SessionId: newResp.SessionId,
	})
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	for i := 0; i < 50; i++ {
		if cancelled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !cancelled {
		t.Errorf("expected session cancel func to be triggered")
	}
}
