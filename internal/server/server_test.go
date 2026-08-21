package server

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
		Provider:      "mock",
		Model:         "mock-model",
		NaturalPacing: &pacing,
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
		Provider:      "mock",
		Model:         "mock-model",
		NaturalPacing: &pacing,
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
