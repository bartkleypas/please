package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// RemoteDaemonProvider connects to a running Please engine daemon over HTTP/HTTPS and SSE.
type RemoteDaemonProvider struct {
	BaseURL    string
	AuthToken  string
	CACertPath string
	client     *http.Client
}

// NewRemoteDaemonProvider creates a new provider instance connected to the specified daemon base URL.
func NewRemoteDaemonProvider(baseURL, authToken, caCertPath string) (*RemoteDaemonProvider, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

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

// GenerateResponseStream streams responses character-by-character from the daemon SSE endpoint.
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

		// 1. Determine last message content & images
		userMessage := ""
		role := RoleUser
		var images []string

		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			userMessage = lastMsg.Content
			role = lastMsg.Role
			images = lastMsg.Images
		}

		payload := map[string]interface{}{
			"message":        userMessage,
			"role":           string(role),
			"images":         images,
			"max_tool_depth": 10,
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			errChan <- fmt.Errorf("failed to encode stream request payload: %w", err)
			return
		}

		endpoint := fmt.Sprintf("%s/api/v1/chat/stream", p.BaseURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			errChan <- fmt.Errorf("failed to create HTTP request: %w", err)
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if p.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+p.AuthToken)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			errChan <- fmt.Errorf("failed to connect to daemon at %s: %w", endpoint, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errBody := make([]byte, 512)
			n, _ := resp.Body.Read(errBody)
			errChan <- fmt.Errorf("daemon returned error (%d): %s", resp.StatusCode, strings.TrimSpace(string(errBody[:n])))
			return
		}

		// 2. Read SSE stream line-by-line
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
