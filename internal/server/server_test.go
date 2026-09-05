package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/engine"
)

func TestCertGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	hosts := []string{"bart-mac.local", "192.168.1.50"}

	bundle, err := Generate20YearCerts(tmpDir, hosts)
	if err != nil {
		t.Fatalf("Generate20YearCerts failed: %v", err)
	}

	// Verify CA cert
	caPEM, err := os.ReadFile(bundle.CACertPath)
	if err != nil {
		t.Fatalf("failed to read CA cert: %v", err)
	}
	caBlock, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}
	if !caCert.IsCA {
		t.Errorf("expected IsCA to be true")
	}
	// Verify ~20 years expiration
	years := caCert.NotAfter.Sub(caCert.NotBefore).Hours() / 24 / 365
	if years < 19.9 || years > 20.1 {
		t.Errorf("expected ~20 years validity, got %.2f years", years)
	}

	// Verify Server cert signed by CA
	serverPEM, err := os.ReadFile(bundle.ServerCertPath)
	if err != nil {
		t.Fatalf("failed to read server cert: %v", err)
	}
	serverBlock, _ := pem.Decode(serverPEM)
	serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse server cert: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	opts := x509.VerifyOptions{
		Roots:   roots,
		DNSName: "bart-mac.local",
	}
	if _, err := serverCert.Verify(opts); err != nil {
		t.Errorf("server cert failed verification against root CA: %v", err)
	}
}

func TestAuthMiddleware(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	mgr := engine.NewManager(engine.NewGraph(), storage)
	srv := NewServer(mgr)

	handler := srv.Handler()

	// 1. Without auth token set: all requests succeed
	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK without auth token configured, got %d", w.Code)
	}

	// 2. Set auth token
	srv.SetAuthToken("test-secret-token-123")

	// Missing token -> 401
	req = httptest.NewRequest("GET", "/api/v1/graph", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for missing token, got %d", w.Code)
	}

	// Invalid token -> 401
	req = httptest.NewRequest("GET", "/api/v1/graph", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for invalid token, got %d", w.Code)
	}

	// Valid Bearer token -> 200
	req = httptest.NewRequest("GET", "/api/v1/graph", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token-123")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for valid Bearer token, got %d", w.Code)
	}

	// Valid query token -> 200
	req = httptest.NewRequest("GET", "/api/v1/graph?token=test-secret-token-123", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for valid ?token= query, got %d", w.Code)
	}

	// Health check bypasses auth -> 200
	req = httptest.NewRequest("GET", "/api/v1/health", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK on healthcheck without token, got %d", w.Code)
	}
}

func TestRESTAPI_V1(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	mgr := engine.NewManager(engine.NewGraph(), storage)
	mgr.RegisterDefaultTools(tmpDir)

	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Summary of discussion",
	}
	pacing := false
	cfg := &engine.Config{
		Server: &engine.ServerConfig{
			Provider: "mock",
			Model:    "mock-model",
		},
		Client: &engine.ClientConfig{
			NaturalPacing: &pacing,
		},
	}
	srv := NewServerWithProvider(mgr, mockProvider, cfg)
	handler := srv.Handler()

	// 1. Healthcheck
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on healthcheck, got %d", w.Code)
	}

	// 2. Create Node via POST /api/v1/nodes
	nodePayload := `{"parent_id":"","role":"system","content":"System instructions"}`
	req = httptest.NewRequest("POST", "/api/v1/nodes", strings.NewReader(nodePayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on node creation, got %d: %s", w.Code, w.Body.String())
	}
	var createdNode engine.Node
	_ = json.NewDecoder(w.Body).Decode(&createdNode)
	if createdNode.ID == "" || createdNode.Content != "System instructions" {
		t.Errorf("unexpected created node: %+v", createdNode)
	}

	// 3. Get Node via GET /api/v1/nodes/{id}
	req = httptest.NewRequest("GET", "/api/v1/nodes/"+createdNode.ID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on get node, got %d", w.Code)
	}

	// 4. Create child user node
	childPayload := `{"parent_id":"` + createdNode.ID + `","role":"user","content":"User query"}`
	req = httptest.NewRequest("POST", "/api/v1/nodes", strings.NewReader(childPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var childNode engine.Node
	_ = json.NewDecoder(w.Body).Decode(&childNode)

	// 5. Test Supernode Compaction via POST /api/v1/supernodes
	superPayload := `{"node_ids":["` + createdNode.ID + `","` + childNode.ID + `"]}`
	req = httptest.NewRequest("POST", "/api/v1/supernodes", strings.NewReader(superPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on supernode, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Test Prune Branch via DELETE /api/v1/branches/{id}
	req = httptest.NewRequest("DELETE", "/api/v1/branches/"+childNode.ID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on branch prune, got %d", w.Code)
	}

	// 7. Test Garbage Collection via POST /api/v1/gc
	req = httptest.NewRequest("POST", "/api/v1/gc", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on GC, got %d", w.Code)
	}

	// 8. Test Tools List via GET /api/v1/tools
	req = httptest.NewRequest("GET", "/api/v1/tools", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on tools list, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "read_file") {
		t.Errorf("expected tools list to contain read_file, got: %s", w.Body.String())
	}
}

func TestChatStream_SSE(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	mgr := engine.NewManager(engine.NewGraph(), storage)

	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Hello from streaming AI!",
		ResponseThought: "Internal reasoning stream",
	}
	pacing := false
	cfg := &engine.Config{
		Server: &engine.ServerConfig{
			Provider: "mock",
			Model:    "mock-model",
		},
		Client: &engine.ClientConfig{
			NaturalPacing: &pacing,
		},
	}
	srv := NewServerWithProvider(mgr, mockProvider, cfg)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Issue SSE request
	reqBody := `{"message":"Hello streaming server!","role":"user"}`
	resp, err := http.Post(ts.URL+"/api/v1/chat/stream", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to post chat stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from SSE endpoint, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %s", contentType)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	rawSSE := buf.String()

	// Verify events in SSE output
	if !strings.Contains(rawSSE, "event: token") {
		t.Errorf("expected SSE to contain event: token, got:\n%s", rawSSE)
	}
	if !strings.Contains(rawSSE, "event: node_complete") {
		t.Errorf("expected SSE to contain event: node_complete, got:\n%s", rawSSE)
	}

	// Verify graph has both user and assistant nodes
	nodes := mgr.Graph.Nodes
	if len(nodes) < 2 {
		t.Errorf("expected at least 2 nodes created in DAG, got %d", len(nodes))
	}
}

func TestChatStream_ReusesExistingUserNode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	mgr := engine.NewManager(engine.NewGraph(), storage)

	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Assistant response",
	}
	pacing := false
	cfg := &engine.Config{
		Server: &engine.ServerConfig{
			Provider: "mock",
			Model:    "mock-model",
		},
		Client: &engine.ClientConfig{
			NaturalPacing: &pacing,
		},
	}
	srv := NewServerWithProvider(mgr, mockProvider, cfg)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Client creates system node and user node first
	sysNode, err := mgr.CreateNode("", engine.RoleSystem, "System prompt", false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	userNode, err := mgr.CreateNode(sysNode.ID, engine.RoleUser, "Client created message", false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}

	// 2. Client initiates stream passing node_id
	clientProvider, err := engine.NewRemoteDaemonProvider(ts.URL, "", "")
	if err != nil {
		t.Fatalf("failed to create client provider: %v", err)
	}

	messages := []engine.Message{
		{ID: sysNode.ID, Role: engine.RoleSystem, Content: "System prompt"},
		{ID: userNode.ID, ParentID: sysNode.ID, Role: engine.RoleUser, Content: "Client created message"},
	}

	contentChan, _, _, errChan := clientProvider.GenerateResponseStream(context.Background(), messages, nil)
	for contentChan != nil || errChan != nil {
		select {
		case _, ok := <-contentChan:
			if !ok {
				contentChan = nil
			}
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			}
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}

	// 3. Verify exactly 3 nodes exist in total: 1 System, 1 User, 1 Assistant
	nodes := mgr.Graph.Nodes
	if len(nodes) != 3 {
		t.Fatalf("expected exactly 3 nodes in DAG, got %d", len(nodes))
	}

	// Verify the children of the system node is only the original userNode
	sysChildren := mgr.GetChildren(sysNode.ID)
	if len(sysChildren) != 1 || sysChildren[0].ID != userNode.ID {
		t.Errorf("expected system node to have only 1 child (%s), got %+v", userNode.ID, sysChildren)
	}

	// Verify assistant node's parent is the original userNode
	userChildren := mgr.GetChildren(userNode.ID)
	if len(userChildren) != 1 || userChildren[0].Role != engine.RoleAssistant {
		t.Errorf("expected user node to have 1 assistant child, got %+v", userChildren)
	}
}

func TestServerAutoSync(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "vault-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	storage, err := engine.NewSQLiteStorage(tmpFile.Name(), "")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)
	srv := NewServer(mgr)

	node, err := mgr.CreateNode("", engine.RoleSystem, "Initial Prompt", false)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	externalStorage, _ := engine.NewSQLiteStorage(tmpFile.Name(), "")
	externalNode := &engine.Node{
		ID:       "external-node",
		ParentID: node.ID,
		Role:     engine.RoleUser,
		Content:  "External Message",
	}
	if err := externalStorage.SaveNode(externalNode); err != nil {
		t.Fatalf("failed to save external node: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/graph", nil)
	w := httptest.NewRecorder()
	srv.handleGraph(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", w.Code)
	}

	var respGraph engine.Graph
	if err := json.NewDecoder(w.Body).Decode(&respGraph); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := respGraph.Nodes["external-node"]; !ok {
		t.Error("Auto-Sync failed: external-node not found in server response")
	}
}

func TestChatStream_MultiTurnToolCascading(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	mgr := engine.NewManager(engine.NewGraph(), storage)

	// Register a test tool in manager
	mgr.Registry = engine.NewToolRegistry()
	mgr.Registry.Register(engine.Tool{
		Name:        "test_reader",
		Description: "Reads test files",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "[Lines 1-64 of test content]", nil
		},
	})

	turnCounter := 0
	mockProvider := &engine.MockLLMProvider{
		StreamHandler: func(messages []engine.Message, tools []engine.Tool) (string, string, []engine.ToolCall, error) {
			turnCounter++
			if turnCounter == 1 {
				// Turn 1: Emit tool call
				tCall := engine.ToolCall{
					ID:   "call_123",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "test_reader",
						Arguments: json.RawMessage(`{"path":"test.txt"}`),
					},
				}
				return "", "I will read the test file first.", []engine.ToolCall{tCall}, nil
			}

			// Turn 2: Verify tool result was delivered in messages context
			hasToolResult := false
			for _, m := range messages {
				if m.Role == engine.RoleTool && strings.Contains(m.Content, "[Lines 1-64 of test content]") {
					hasToolResult = true
					break
				}
			}
			if !hasToolResult {
				return "", "", nil, fmt.Errorf("Turn 2 did not receive RoleTool observation in context: %+v", messages)
			}

			// Turn 2: Emit final response summary
			return "Here is the summary of the test file: All looks great!", "Finished reading observation.", nil, nil
		},
	}

	pacing := false
	cfg := &engine.Config{
		Server: &engine.ServerConfig{
			Provider: "mock",
			Model:    "mock-model",
		},
		Client: &engine.ClientConfig{
			NaturalPacing: &pacing,
		},
	}
	srv := NewServerWithProvider(mgr, mockProvider, cfg)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Connect client to daemon
	clientProvider, err := engine.NewRemoteDaemonProvider(ts.URL, "", "")
	if err != nil {
		t.Fatalf("failed to create client provider: %v", err)
	}

	messages := []engine.Message{
		{Role: engine.RoleUser, Content: "Please summarize test.txt"},
	}

	contentChan, thoughtChan, toolCallChan, errChan := clientProvider.GenerateResponseStream(context.Background(), messages, nil)

	var allContent string
	var allThought string

	for contentChan != nil || thoughtChan != nil || toolCallChan != nil || errChan != nil {
		select {
		case chunk, ok := <-contentChan:
			if !ok {
				contentChan = nil
				continue
			}
			allContent += chunk
		case thought, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
				continue
			}
			allThought += thought
		case _, ok := <-toolCallChan:
			if !ok {
				toolCallChan = nil
				continue
			}
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}

	if !strings.Contains(allContent, "Here is the summary of the test file") {
		t.Errorf("expected final content to contain summary, got: %s", allContent)
	}
	if !strings.Contains(allThought, "test_reader") {
		t.Errorf("expected thought to contain tool execution progress, got: %s", allThought)
	}
	if turnCounter != 2 {
		t.Errorf("expected 2 turns executed on server, got %d", turnCounter)
	}
}

func TestRemoteDaemonStorage_CreateSupernode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	graph := engine.NewGraph()
	mgr := engine.NewManager(graph, storage)

	root, _ := mgr.CreateNode("", engine.RoleSystem, "You are George 🦉📚", false)
	user, _ := mgr.CreateNode(root.ID, engine.RoleUser, "Investigate logs", false)
	asst, _ := mgr.CreateAssistantNode(user.ID, "Logs clear 🔍📜", "", nil, false)

	mockProvider := &engine.MockLLMProvider{
		ResponseContent: "Milestone: Investigation complete.",
	}

	cfg := engine.NewDefaultConfig()
	srv := NewServerWithProvider(mgr, mockProvider, cfg)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	remoteStorage, err := engine.NewRemoteDaemonStorage(ts.URL, "", "")
	if err != nil {
		t.Fatalf("failed to create remote storage: %v", err)
	}

	superNode, err := remoteStorage.CreateSupernode(context.Background(), []string{user.ID, asst.ID}, "focus on errors")
	if err != nil {
		t.Fatalf("expected CreateSupernode to succeed, got: %v", err)
	}

	if superNode.Role != engine.RoleSummary {
		t.Errorf("expected supernode role to be RoleSummary, got %s", superNode.Role)
	}
	if !strings.Contains(superNode.Content, "🎯 Trajectory: 🔍📜") {
		t.Errorf("expected supernode to contain trajectory, got: %s", superNode.Content)
	}
}

func TestChatStream_WithAmbientTelemetryContext(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := engine.NewSQLiteStorage(dbPath, "")
	mgr := engine.NewManager(engine.NewGraph(), storage)
	mgr.AmbientTelemetry = true
	mgr.WorkspaceDir = tmpDir
	rootNode, _ := mgr.CreateNode("", engine.RoleSystem, "You are George the Archivist.", false)

	var capturedMessages []engine.Message
	mockProvider := &engine.MockLLMProvider{
		StreamHandler: func(messages []engine.Message, tools []engine.Tool) (string, string, []engine.ToolCall, error) {
			capturedMessages = messages
			return "I received your telemetry.", "", nil, nil
		},
	}

	pacing := false
	enabled := true
	cfg := &engine.Config{
		Server: &engine.ServerConfig{
			Provider:         "mock",
			Model:            "mock-model",
			AmbientTelemetry: &enabled,
		},
		Client: &engine.ClientConfig{
			NaturalPacing: &pacing,
		},
	}
	srv := NewServerWithProvider(mgr, mockProvider, cfg)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Issue SSE request with active_file and cursor_line
	reqBody := fmt.Sprintf(`{
		"parent_id": %q,
		"message": "Where are we?",
		"role": "user",
		"active_file": "internal/engine/service.go",
		"cursor_line": 42
	}`, rootNode.ID)
	resp, err := http.Post(ts.URL+"/api/v1/chat/stream", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to post chat stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from SSE endpoint, got %d", resp.StatusCode)
	}

	// Drain response
	_, _ = io.ReadAll(resp.Body)

	// Verify captured messages by mock LLM
	if len(capturedMessages) == 0 {
		t.Fatal("expected LLM provider to receive messages")
	}

	// Verify Genesis message received AmbientTelemetryContract
	if !strings.Contains(capturedMessages[0].Content, engine.AmbientTelemetryContract) {
		t.Errorf("expected Genesis root to contain AmbientTelemetryContract, got: %q", capturedMessages[0].Content)
	}

	leaf := capturedMessages[len(capturedMessages)-1]
	if leaf.Role != engine.RoleUser {
		t.Errorf("expected leaf to be RoleUser, got %s", leaf.Role)
	}
	if !strings.Contains(leaf.Content, "<USER_REQUEST>\nWhere are we?\n</USER_REQUEST>") {
		t.Errorf("expected leaf to contain USER_REQUEST envelope, got: %q", leaf.Content)
	}
	if !strings.Contains(leaf.Content, "<ADDITIONAL_METADATA>") {
		t.Errorf("expected leaf to contain ADDITIONAL_METADATA, got: %q", leaf.Content)
	}
	if !strings.Contains(leaf.Content, "active_file: internal/engine/service.go") {
		t.Errorf("expected leaf to contain active_file, got: %q", leaf.Content)
	}
	if !strings.Contains(leaf.Content, "cursor_line: 42") {
		t.Errorf("expected leaf to contain cursor_line, got: %q", leaf.Content)
	}

	// Invariant: After stream finishes, Manager's temporary clientContext is cleanly reset
	if mgr.GetClientContext() != nil {
		t.Errorf("expected Manager.clientContext to be reset to nil after turn completion, got: %v", mgr.GetClientContext())
	}
}
