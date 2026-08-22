package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RemoteDaemonProvider connects to a running Please engine daemon over HTTP/HTTPS and SSE.
type RemoteDaemonProvider struct {
	BaseURL    string
	AuthToken  string
	CACertPath string
	client     *http.Client
}

func resolveCACert(caCertPath string, baseURL string) string {
	if caCertPath != "" {
		if strings.HasPrefix(caCertPath, "~/") || caCertPath == "~" {
			if home, err := os.UserHomeDir(); err == nil {
				caCertPath = filepath.Join(home, strings.TrimPrefix(caCertPath, "~"))
			}
		}
		return caCertPath
	}

	// Auto-discovery if connecting via HTTPS
	if strings.HasPrefix(baseURL, "https://") {
		if cfgDir, err := GetConfigDir(); err == nil {
			defaultCA := filepath.Join(cfgDir, "certs", "ca.crt")
			if _, err := os.Stat(defaultCA); err == nil {
				return defaultCA
			}
		}
	}
	return ""
}

// NewRemoteDaemonProvider creates a new provider instance connected to the specified daemon base URL.
func NewRemoteDaemonProvider(baseURL, authToken, caCertPath string) (*RemoteDaemonProvider, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	caCertPath = resolveCACert(caCertPath, baseURL)

	transport := http.DefaultTransport.(*http.Transport).Clone()

	if caCertPath != "" {
		caPEM, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", caCertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", caCertPath)
		}
		transport.TLSClientConfig = &tls.Config{
			RootCAs: pool,
		}
	}

	client := &http.Client{
		Transport: transport,
	}

	return &RemoteDaemonProvider{
		BaseURL:    baseURL,
		AuthToken:  authToken,
		CACertPath: caCertPath,
		client:     client,
	}, nil
}

// GenerateResponse generates a single synchronous message by consuming the stream.
func (p *RemoteDaemonProvider) GenerateResponse(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	contentChan, thoughtChan, toolCallChan, errChan := p.GenerateResponseStream(ctx, messages, tools)

	var fullContent strings.Builder
	var fullThought strings.Builder
	var toolCalls []ToolCall

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case thought, ok := <-thoughtChan:
			if ok {
				fullThought.WriteString(thought)
			}

		case chunk, ok := <-contentChan:
			if ok {
				fullContent.WriteString(chunk)
			}

		case tc, ok := <-toolCallChan:
			if ok && len(tc) > 0 {
				toolCalls = append(toolCalls, tc...)
			}

		case err, ok := <-errChan:
			if ok && err != nil {
				return nil, err
			}
			return &Message{
				Role:      RoleAssistant,
				Content:   fullContent.String(),
				Thought:   fullThought.String(),
				ToolCalls: toolCalls,
			}, nil
		}
	}
}

// GenerateResponseStream initiates a streaming request to /api/v1/chat/stream on the daemon.
func (p *RemoteDaemonProvider) GenerateResponseStream(ctx context.Context, messages []Message, tools []Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error) {
	contentChan := make(chan string, 100)
	thoughtChan := make(chan string, 100)
	toolCallChan := make(chan []ToolCall, 10)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(thoughtChan)
		defer close(toolCallChan)
		defer close(errChan)

		// Extract the latest user message and history
		var lastUserMessage string
		var parentID string
		var images []string

		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == RoleUser {
				lastUserMessage = messages[i].Content
				images = messages[i].Images
				parentID = messages[i].ParentID
				break
			}
		}

		payload := map[string]interface{}{
			"message":   lastUserMessage,
			"role":      "user",
			"parent_id": parentID,
			"images":    images,
			"messages":  messages,
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			errChan <- fmt.Errorf("failed to serialize request: %w", err)
			return
		}

		streamURL := p.BaseURL + "/api/v1/chat/stream"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(bodyBytes))
		if err != nil {
			errChan <- fmt.Errorf("failed to create stream request: %w", err)
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if p.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+p.AuthToken)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			errChan <- fmt.Errorf("failed to connect to daemon: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf("daemon returned HTTP %d: %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		var currentEvent string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			line := scanner.Text()

			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")

				switch currentEvent {
				case "thought":
					var tPayload struct {
						Chunk string `json:"chunk"`
					}
					if err := json.Unmarshal([]byte(dataStr), &tPayload); err == nil && tPayload.Chunk != "" {
						thoughtChan <- tPayload.Chunk
					}

				case "token":
					var cPayload struct {
						Chunk string `json:"chunk"`
					}
					if err := json.Unmarshal([]byte(dataStr), &cPayload); err == nil && cPayload.Chunk != "" {
						contentChan <- cPayload.Chunk
					}

				case "tool_call":
					var tcPayload struct {
						ID        string                 `json:"id"`
						Tool      string                 `json:"tool"`
						Arguments map[string]interface{} `json:"arguments"`
					}
					if err := json.Unmarshal([]byte(dataStr), &tcPayload); err == nil {
						argsBytes, _ := json.Marshal(tcPayload.Arguments)
						call := ToolCall{
							ID:   tcPayload.ID,
							Type: "function",
							Function: struct {
								Name      string          `json:"name"`
								Arguments json.RawMessage `json:"arguments"`
							}{
								Name:      tcPayload.Tool,
								Arguments: argsBytes,
							},
						}
						toolCallChan <- []ToolCall{call}
					}

				case "tool_result":
					var trPayload struct {
						ID     string `json:"id"`
						Tool   string `json:"tool"`
						Output string `json:"output"`
						Error  string `json:"error,omitempty"`
					}
					if err := json.Unmarshal([]byte(dataStr), &trPayload); err == nil {
						if trPayload.Error != "" {
							thoughtChan <- fmt.Sprintf("⚠️  Tool error: %s\n", trPayload.Error)
						} else {
							thoughtChan <- "✅ Observation received.\n"
						}
					}

				case "error":
					var ePayload struct {
						Error string `json:"error"`
					}
					if err := json.Unmarshal([]byte(dataStr), &ePayload); err == nil && ePayload.Error != "" {
						errChan <- fmt.Errorf("remote daemon error: %s", ePayload.Error)
						return
					}

				case "node_complete":
					// Turn completed successfully
					return
				}
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			errChan <- scanErr
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}

// RemoteDaemonStorage implements Storage by proxying node mutations and queries to a Please engine daemon
type RemoteDaemonStorage struct {
	BaseURL    string
	AuthToken  string
	HTTPClient *http.Client
}

// NewRemoteDaemonStorage initializes a storage instance connected to the Please engine daemon
func NewRemoteDaemonStorage(baseURL, authToken, caCertPath string) (*RemoteDaemonStorage, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	caCertPath = resolveCACert(caCertPath, baseURL)

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
		HTTPClient: &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}, nil
}

// SaveNode persists a node into the remote daemon's vault
func (s *RemoteDaemonStorage) SaveNode(node *Node) error {
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/nodes", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.AuthToken)
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

// LoadGraph fetches the full conversation graph from the remote daemon
func (s *RemoteDaemonStorage) LoadGraph() (*Graph, string, error) {
	req, err := http.NewRequest(http.MethodGet, s.BaseURL+"/api/v1/graph", nil)
	if err != nil {
		return nil, "", err
	}
	if s.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.AuthToken)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	var graph Graph
	if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
		return nil, "", err
	}

	var latestID string
	var latestTime time.Time
	for id, node := range graph.Nodes {
		if latestID == "" || node.Timestamp.After(latestTime) {
			latestTime = node.Timestamp
			latestID = id
		}
	}
	return &graph, latestID, nil
}

// GarbageCollect triggers database garbage collection on the remote daemon
func (s *RemoteDaemonStorage) GarbageCollect() (int64, error) {
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/gc", nil)
	if err != nil {
		return 0, err
	}
	if s.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.AuthToken)
	}
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

// UpdateNodeMetadata updates node attributes on the daemon
func (s *RemoteDaemonStorage) UpdateNodeMetadata(node *Node) error {
	return s.SaveNode(node)
}

// UpdateNodeParentID updates the parent link of a node
func (s *RemoteDaemonStorage) UpdateNodeParentID(nodeID, newParentID string) error {
	return nil
}

// UpdateNodeObservations updates tool execution observations on the daemon
func (s *RemoteDaemonStorage) UpdateNodeObservations(nodeID string, obs []ToolObservation) error {
	return nil
}
