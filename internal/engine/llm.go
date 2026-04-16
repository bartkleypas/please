package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Message represents a simplified message for LLM providers
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// LLMProvider defines the interface for interacting with different AI backends
type LLMProvider interface {
	GenerateResponse(ctx context.Context, messages []Message) (string, error)
	GenerateResponseStream(ctx context.Context, messages []Message) (<-chan string, <-chan error)
}

// OllamaProvider implements LLMProvider for local Ollama instances
type OllamaProvider struct {
	Endpoint string
	Model    string
	client   *http.Client
}

func NewOllamaProvider(endpoint, model string) *OllamaProvider {
	return &OllamaProvider{
		Endpoint: endpoint,
		Model:    model,
		client: &http.Client{
			// We don't set a global timeout here because we want to
			// control the timeout via the context passed to GenerateResponse.
		},
	}
}

type ollamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ollamaResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

func (o *OllamaProvider) GenerateResponse(ctx context.Context, messages []Message) (string, error) {
	reqBody := ollamaRequest{
		Model:    o.Model,
		Messages: messages,
		Stream:   false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.Endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	// Ensure the body is closed and drained.
	// Draining the body is crucial for connection reuse and signaling completion.
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned non-ok status: %d", resp.StatusCode)
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return ollamaResp.Message.Content, nil
}

func (o *OllamaProvider) GenerateResponseStream(ctx context.Context, messages []Message) (<-chan string, <-chan error) {
	contentChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(errChan)

		reqBody := ollamaRequest{
			Model:    o.Model,
			Messages: messages,
			Stream:   true,
		}

		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			errChan <- fmt.Errorf("failed to marshal request: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", o.Endpoint, bytes.NewBuffer(jsonBody))
		if err != nil {
			errChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.client.Do(req)
		if err != nil {
			errChan <- fmt.Errorf("request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errChan <- fmt.Errorf("ollama returned non-ok status: %d", resp.StatusCode)
			return
		}

		decoder := json.NewDecoder(resp.Body)
		for {
			var ollamaResp ollamaResponse
			if err := decoder.Decode(&ollamaResp); err != nil {
				if err == io.EOF {
					break
				}
				errChan <- fmt.Errorf("failed to decode streaming response: %w", err)
				return
			}

			contentChan <- ollamaResp.Message.Content
			if ollamaResp.Done {
				break
			}
		}
	}()

	return contentChan, errChan
}
