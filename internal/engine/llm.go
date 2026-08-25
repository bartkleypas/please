package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Message represents a simplified message for LLM providers
type Message struct {
	ID           string            `json:"id,omitempty"`
	ParentID     string            `json:"parent_id,omitempty"`
	Role         Role              `json:"role"`
	Content      string            `json:"content"`
	Thought      string            `json:"thought,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	Observations []ToolObservation `json:"observations,omitempty"`
	Internal     bool              `json:"internal,omitempty"`
	Images       []string          `json:"images,omitempty"`
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
	Options  *ModelOptions
	client   *http.Client
}

func NewOllamaProvider(endpoint, model string, options *ModelOptions) *OllamaProvider {
	return &OllamaProvider{
		Endpoint: endpoint,
		Model:    model,
		Options:  options,
		client:   &http.Client{
			// We don't set a global timeout here because we want to
			// control the timeout via the context passed to GenerateResponse.
		},
	}
}

func (o *OllamaProvider) buildOllamaOptions() map[string]interface{} {
	if o.Options == nil {
		return nil
	}
	opts := make(map[string]interface{})
	if o.Options.Temperature != nil {
		opts["temperature"] = *o.Options.Temperature
	}
	if o.Options.TopP != nil {
		opts["top_p"] = *o.Options.TopP
	}
	if o.Options.TopK != nil {
		opts["top_k"] = *o.Options.TopK
	}
	if o.Options.NumCtx != nil {
		opts["num_ctx"] = *o.Options.NumCtx
	}
	if o.Options.MaxTokens != nil {
		opts["num_predict"] = *o.Options.MaxTokens
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
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
	Model    string                 `json:"model"`
	Messages []ollamaMessage        `json:"messages"`
	Stream   bool                   `json:"stream"`
	Tools    []ollamaTool           `json:"tools,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Thinking   string           `json:"thinking,omitempty"`
	Reasoning  string           `json:"reasoning,omitempty"`
	ToolCalls  []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Images     []string         `json:"images,omitempty"`
}

func encodeImageToBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

type ollamaToolCall struct {
	ID       string `json:"id,omitempty"`
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
		Options:  o.buildOllamaOptions(),
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
			ID:   tc.ID,
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
			Options:  o.buildOllamaOptions(),
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
						ID:   tc.ID,
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
				// Hard halt: immediately stop streaming if tool calls are emitted
				break
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
				ID: tc.ID,
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}

		var base64Images []string
		for _, imgPath := range m.Images {
			b64, err := encodeImageToBase64(imgPath)
			if err == nil {
				base64Images = append(base64Images, b64)
			} else {
				fmt.Printf("Warning: failed to read image for ollama payload: %v\n", err)
			}
		}

		roleStr := string(m.Role)
		contentStr := m.Content
		if m.Role == RoleSummary {
			roleStr = "system"
			contentStr = "[Conversation Milestone & Summary Context]:\n" + m.Content
		}

		out = append(out, ollamaMessage{
			Role:       roleStr,
			Content:    contentStr,
			ToolCalls:  tCalls,
			ToolCallID: m.ToolCallID,
			Images:     base64Images,
		})
	}

	return out
}
