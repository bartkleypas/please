package storage

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/google/uuid"
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
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/gc", nil)
	if err != nil {
		return 0, err
	}
	s.applyHeaders(req)
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		DeletedNodes int64 `json:"deleted_nodes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
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
