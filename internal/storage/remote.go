package storage

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/google/uuid"
)

var (
	_ Storage          = (*RemoteDaemonStorage)(nil)
	_ MemoryStore      = (*RemoteDaemonStorage)(nil)
	_ ObservationStore = (*RemoteDaemonStorage)(nil)
)

// RemoteDaemonStorage implements Storage by proxying node mutations and queries to a Please engine daemon.
type RemoteDaemonStorage struct {
	BaseURL    string
	AuthToken  string
	SessionID  string
	HTTPClient *http.Client
}

// NewRemoteDaemonStorage initializes a storage instance connected to the Please engine daemon.
func NewRemoteDaemonStorage(baseURL, authToken, caCertPath string) (*RemoteDaemonStorage, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	caCertPath = domain.ResolveCACert(caCertPath, baseURL)

	transport := http.DefaultTransport.(*http.Transport).Clone()

	if caCertPath != "" {
		caPEM, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
	}

	return &RemoteDaemonStorage{
		BaseURL:    baseURL,
		AuthToken:  authToken,
		SessionID:  uuid.New().String(),
		HTTPClient: &http.Client{Transport: transport},
	}, nil
}

func (s *RemoteDaemonStorage) applyHeaders(req *http.Request) {
	if s.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.AuthToken)
	}
	if s.SessionID != "" {
		req.Header.Set("X-Please-Session-ID", s.SessionID)
	}
}

// SaveNode persists a node into the remote daemon's vault.
func (s *RemoteDaemonStorage) SaveNode(node *graph.Node) error {
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/nodes", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	s.applyHeaders(req)
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon error (%d): %s", resp.StatusCode, string(respBytes))
	}
	return nil
}

// LoadGraph fetches the full conversation graph from the remote daemon.
func (s *RemoteDaemonStorage) LoadGraph() (*graph.Graph, string, error) {
	req, err := http.NewRequest(http.MethodGet, s.BaseURL+"/api/v1/graph", nil)
	if err != nil {
		return nil, "", err
	}
	s.applyHeaders(req)
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	var g graph.Graph
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return nil, "", err
	}

	var latestID string
	var latestTime time.Time
	for id, node := range g.Nodes {
		if latestID == "" || node.Timestamp.After(latestTime) {
			latestTime = node.Timestamp
			latestID = id
		}
	}
	return &g, latestID, nil
}

// GarbageCollect triggers database garbage collection on the remote daemon.
func (s *RemoteDaemonStorage) GarbageCollect() (int64, error) {
	var result struct {
		PurgedRecords int64 `json:"purged_records"`
		DeletedNodes  int64 `json:"deleted_nodes"`
	}
	if _, err := s.doJSON(http.MethodPost, "/api/v1/gc", nil, &result); err != nil {
		return 0, err
	}
	if result.PurgedRecords != 0 {
		return result.PurgedRecords, nil
	}
	return result.DeletedNodes, nil
}

// UpdateNodeMetadata updates node attributes on the daemon.
func (s *RemoteDaemonStorage) UpdateNodeMetadata(node *graph.Node) error {
	return s.SaveNode(node)
}

// UpdateNodeParentID updates the parent link of a node.
func (s *RemoteDaemonStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	return nil
}

// UpdateNodeObservations updates tool execution observations on the daemon.
func (s *RemoteDaemonStorage) UpdateNodeObservations(nodeID string, obs []domain.ToolObservation) error {
	return nil
}

// CreateSupernode calls POST /api/v1/supernodes on the daemon.
func (s *RemoteDaemonStorage) CreateSupernode(ctx context.Context, nodeIDs []string, directive string) (*graph.Node, error) {
	payload := map[string]interface{}{
		"node_ids":  nodeIDs,
		"directive": directive,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize supernode payload: %w", err)
	}

	url := s.BaseURL + "/api/v1/supernodes"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	s.applyHeaders(req)
	if s.HTTPClient == nil {
		s.HTTPClient = &http.Client{}
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("supernode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon error (%d): %s", resp.StatusCode, string(respBody))
	}

	var superNode graph.Node
	if err := json.NewDecoder(resp.Body).Decode(&superNode); err != nil {
		return nil, fmt.Errorf("failed to decode supernode response: %w", err)
	}

	return &superNode, nil
}

// SaveSessionHead updates the session head on the daemon via POST /api/v1/sessions.
func (s *RemoteDaemonStorage) SaveSessionHead(sessionID, nodeID string) error {
	if sessionID == "" {
		sessionID = "main"
	}
	payload := map[string]string{
		"session_id":   sessionID,
		"head_node_id": nodeID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/sessions", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	s.applyHeaders(req)
	if s.HTTPClient == nil {
		s.HTTPClient = &http.Client{}
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon error (%d): %s", resp.StatusCode, string(respBytes))
	}
	return nil
}

// GetSessionHead retrieves the head node ID for a session from GET /api/v1/sessions/{id}. Returns ("", nil) if not found.
func (s *RemoteDaemonStorage) GetSessionHead(sessionID string) (string, error) {
	if sessionID == "" {
		sessionID = "main"
	}
	req, err := http.NewRequest(http.MethodGet, s.BaseURL+"/api/v1/sessions/"+sessionID, nil)
	if err != nil {
		return "", err
	}
	s.applyHeaders(req)
	if s.HTTPClient == nil {
		s.HTTPClient = &http.Client{}
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("daemon error (%d): %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		SessionID  string `json:"session_id"`
		HeadNodeID string `json:"head_node_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.HeadNodeID, nil
}

// ListSessions retrieves the mapping of all active sessions from GET /api/v1/sessions.
func (s *RemoteDaemonStorage) ListSessions() (map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, s.BaseURL+"/api/v1/sessions", nil)
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req)
	if s.HTTPClient == nil {
		s.HTTPClient = &http.Client{}
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon error (%d): %s", resp.StatusCode, string(respBytes))
	}

	var sessions map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *RemoteDaemonStorage) doJSON(method, path string, body interface{}, out interface{}) (int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, s.BaseURL+path, bodyReader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	s.applyHeaders(req)

	client := s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("daemon error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// SaveMemory inserts or updates a memory record on the remote daemon via POST /api/v1/memories.
func (s *RemoteDaemonStorage) SaveMemory(mem *Memory) error {
	if mem == nil {
		return fmt.Errorf("cannot save nil memory")
	}
	_, err := s.doJSON(http.MethodPost, "/api/v1/memories", mem, mem)
	return err
}

// GetMemory retrieves a memory record by scope, sessionID, and key via GET /api/v1/memories?exact=true.
func (s *RemoteDaemonStorage) GetMemory(scope MemoryScope, sessionID, key string) (*Memory, error) {
	q := url.Values{}
	q.Set("exact", "true")
	if scope != "" {
		q.Set("scope", string(scope))
	}
	if sessionID != "" {
		q.Set("session_id", sessionID)
	}
	q.Set("key", key)

	var mem Memory
	status, err := s.doJSON(http.MethodGet, "/api/v1/memories?"+q.Encode(), nil, &mem)
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mem, nil
}

// QueryMemories retrieves memories matching filter criteria via GET /api/v1/memories.
func (s *RemoteDaemonStorage) QueryMemories(filter MemoryFilter) ([]Memory, error) {
	q := url.Values{}
	if filter.Query != "" {
		q.Set("query", filter.Query)
	}
	if filter.Key != "" {
		q.Set("key", filter.Key)
	}
	if filter.Category != "" {
		q.Set("category", string(filter.Category))
	}
	if filter.Scope != "" {
		q.Set("scope", string(filter.Scope))
	}
	if filter.SessionID != "" {
		q.Set("session_id", filter.SessionID)
	}
	for _, t := range filter.Tags {
		q.Add("tag", t)
	}
	if filter.Limit > 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	if filter.Touch {
		q.Set("touch", "true")
	}

	path := "/api/v1/memories"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var mems []Memory
	if _, err := s.doJSON(http.MethodGet, path, nil, &mems); err != nil {
		return nil, err
	}
	return mems, nil
}

// DeleteMemory deletes a memory entry on the remote daemon via DELETE /api/v1/memories.
func (s *RemoteDaemonStorage) DeleteMemory(scope MemoryScope, sessionID, key string) error {
	q := url.Values{}
	if scope != "" {
		q.Set("scope", string(scope))
	}
	if sessionID != "" {
		q.Set("session_id", sessionID)
	}
	q.Set("key", key)

	status, err := s.doJSON(http.MethodDelete, "/api/v1/memories?"+q.Encode(), nil, nil)
	if status == http.StatusNotFound {
		return ErrMemoryNotFound
	}
	return err
}

// DiagnoseMemories retrieves aggregate memory diagnostics via GET /api/v1/memories/diagnose.
func (s *RemoteDaemonStorage) DiagnoseMemories(scope MemoryScope, sessionID string) (*MemoryDiagnostics, error) {
	q := url.Values{}
	if scope != "" {
		q.Set("scope", string(scope))
	}
	if sessionID != "" {
		q.Set("session_id", sessionID)
	}

	path := "/api/v1/memories/diagnose"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var diag MemoryDiagnostics
	if _, err := s.doJSON(http.MethodGet, path, nil, &diag); err != nil {
		return nil, err
	}
	return &diag, nil
}

// SaveObservationBlob persists an out-of-band sensory payload on the remote daemon via POST /api/v1/observations.
func (s *RemoteDaemonStorage) SaveObservationBlob(blob *ObservationBlob) error {
	if blob == nil {
		return fmt.Errorf("cannot save nil observation blob")
	}
	_, err := s.doJSON(http.MethodPost, "/api/v1/observations", blob, nil)
	return err
}

// GetObservationBlob retrieves an out-of-band sensory payload via GET /api/v1/observations/{receipt_id}.
func (s *RemoteDaemonStorage) GetObservationBlob(receiptID string) (*ObservationBlob, error) {
	var blob ObservationBlob
	if _, err := s.doJSON(http.MethodGet, "/api/v1/observations/"+url.PathEscape(receiptID), nil, &blob); err != nil {
		return nil, err
	}
	return &blob, nil
}
