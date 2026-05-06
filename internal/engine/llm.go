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
	Role         Role              `json:"role"`
	Content      string            `json:"content"`
	Thought      string            `json:"thought,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	Observations []ToolObservation `json:"observations,omitempty"`
	Internal     bool              `json:"internal,omitempty"`
}

// LLMProvider defines the interface for interacting with different AI backends
type LLMProvider interface {
	GenerateResponse(ctx context.Context, messages []Message, tools []Tool) (*Message, error)
	GenerateResponseStream(ctx context.Context, messages []Message, tools []Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error)
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

type ollamaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	Reasoning string           `json:"reasoning,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

func (o *OllamaProvider) GenerateResponse(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	var oTools []ollamaTool
	for _, t := range tools {
		ot := ollamaTool{
			Type: "function",
		}
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		ot.Function.Parameters = t.Parameters
		oTools = append(oTools, ot)
	}

	reqBody := ollamaRequest{
		Model:    o.Model,
		Messages: mapToOllamaMessages(messages),
		Stream:   false,
		Tools:    oTools,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.Endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Ensure the body is closed and drained.
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned non-ok status: %d", resp.StatusCode)
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var tCalls []ToolCall
	for _, tc := range ollamaResp.Message.ToolCalls {
		tCalls = append(tCalls, ToolCall{
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	msg := &Message{
		Role:      RoleAssistant,
		Content:   ollamaResp.Message.Content,
		ToolCalls: tCalls,
	}

	if ollamaResp.Message.Thinking != "" {
		msg.Thought = ollamaResp.Message.Thinking
	} else if ollamaResp.Message.Reasoning != "" {
		msg.Thought = ollamaResp.Message.Reasoning
	}

	return msg, nil
}

func (o *OllamaProvider) GenerateResponseStream(ctx context.Context, messages []Message, tools []Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error) {
	contentChan := make(chan string)
	thoughtChan := make(chan string)
	toolCallChan := make(chan []ToolCall, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(thoughtChan)
		defer close(toolCallChan)
		defer close(errChan)

		var oTools []ollamaTool
		for _, t := range tools {
			ot := ollamaTool{
				Type: "function",
			}
			ot.Function.Name = t.Name
			ot.Function.Description = t.Description
			ot.Function.Parameters = t.Parameters
			oTools = append(oTools, ot)
		}

		reqBody := ollamaRequest{
			Model:    o.Model,
			Messages: mapToOllamaMessages(messages),
			Stream:   true,
			Tools:    oTools,
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
		var collectedToolCalls []ToolCall

		for {
			var ollamaResp ollamaResponse
			if err := decoder.Decode(&ollamaResp); err != nil {
				if err == io.EOF {
					break
				}
				errChan <- fmt.Errorf("failed to decode streaming response: %w", err)
				return
			}

			// Process fields in logical order
			if ollamaResp.Message.Thinking != "" {
				thoughtChan <- ollamaResp.Message.Thinking
			} else if ollamaResp.Message.Reasoning != "" {
				thoughtChan <- ollamaResp.Message.Reasoning
			}

			if ollamaResp.Message.Content != "" {
				contentChan <- ollamaResp.Message.Content
			}

			if len(ollamaResp.Message.ToolCalls) > 0 {
				for _, tc := range ollamaResp.Message.ToolCalls {
					collectedToolCalls = append(collectedToolCalls, ToolCall{
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
				}
			}

			if ollamaResp.Done {
				break
			}
		}

		if len(collectedToolCalls) > 0 {
			toolCallChan <- collectedToolCalls
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}

func mapToOllamaMessages(messages []Message) []ollamaMessage {
	var out []ollamaMessage

	for _, m := range messages {
		// Base message
		var tCalls []ollamaToolCall
		for _, tc := range m.ToolCalls {
			tCalls = append(tCalls, ollamaToolCall{
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}

		out = append(out, ollamaMessage{
			Role:      string(m.Role),
			Content:   m.Content,
			ToolCalls: tCalls,
		})

		// Append side-channel observations as native "tool" responses
		for _, obs := range m.Observations {
			out = append(out, ollamaMessage{
				Role:    "tool",
				Content: obs.Result,
			})
		}
	}

	return out
}
