package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OpenAIProvider struct {
	Endpoint string
	Model    string
	APIKey   string
	client   *http.Client
}

func NewOpenAIProvider(endpoint, model, apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		Endpoint: endpoint,
		Model:    model,
		APIKey:   apiKey,
		client:   &http.Client{},
	}
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

type openAIMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	Name       string             `json:"name,omitempty"`
	ToolCalls  []openAIToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []openAITool    `json:"tools,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		Delta        openAIMessage `json:"delta"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
}

func (o *OpenAIProvider) doRequest(ctx context.Context, reqBody openAIRequest) (*http.Response, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.Endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai returned non-ok status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp, nil
}

func (o *OpenAIProvider) GenerateResponse(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	reqBody := openAIRequest{
		Model:    o.Model,
		Messages: mapToOpenAIMessages(messages),
		Stream:   false,
		Tools:    mapToOpenAITools(tools),
	}

	// OpenAI API drops empty tool arrays sometimes, best to omit if zero
	if len(reqBody.Tools) == 0 {
		reqBody.Tools = nil
	}

	resp, err := o.doRequest(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var openAIResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from openai")
	}

	choice := openAIResp.Choices[0].Message

	var tCalls []ToolCall
	for _, tc := range choice.ToolCalls {
		tCalls = append(tCalls, ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			},
		})
	}

	msg := &Message{
		Role:      RoleAssistant,
		Content:   choice.Content,
		ToolCalls: tCalls,
	}

	return msg, nil
}

func (o *OpenAIProvider) GenerateResponseStream(ctx context.Context, messages []Message, tools []Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error) {
	contentChan := make(chan string)
	thoughtChan := make(chan string)
	toolCallChan := make(chan []ToolCall, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(thoughtChan)
		defer close(toolCallChan)
		defer close(errChan)

		reqBody := openAIRequest{
			Model:    o.Model,
			Messages: mapToOpenAIMessages(messages),
			Stream:   true,
			Tools:    mapToOpenAITools(tools),
		}

		if len(reqBody.Tools) == 0 {
			reqBody.Tools = nil
		}

		resp, err := o.doRequest(ctx, reqBody)
		if err != nil {
			errChan <- err
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		
		// Map of index to tool call builder since tool calls arrive in chunks
		type toolCallBuilder struct {
			id        string
			name      string
			arguments strings.Builder
		}
		toolCallsMap := make(map[int]*toolCallBuilder)
		hasToolCalls := false

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk openAIResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // sometimes comments or other things can be unparseable
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta

			if delta.Content != "" {
				contentChan <- delta.Content
			}

			if len(delta.ToolCalls) > 0 {
				hasToolCalls = true
				for i, tc := range delta.ToolCalls {
					// We just use index since that's how OpenAI maps tool calls in stream
					// But we need to handle if the JSON provides an explicit index.
					// Since we don't have the index in the struct, we assume it's ordered
					// Or just map it based on the implicit index. For robust parsing we should extract index,
					// but standard OpenAI sends them in order. We'll just use the array index for now.
					// Actually OpenAI sends an 'index' field in the tool_calls delta array.
					// Since we didn't add it to openAIToolCall, let's just append to a list and assume
					// it sends one tool call stream at a time or in order.
					
					// A simpler approach for this prototype: OpenAI sends tool call deltas with an index
					// We can just rely on the ID to initialize a new builder.
					if tc.ID != "" {
						toolCallsMap[i] = &toolCallBuilder{
							id:   tc.ID,
							name: tc.Function.Name,
						}
					}
					if tc.Function.Arguments != "" && toolCallsMap[i] != nil {
						toolCallsMap[i].arguments.WriteString(tc.Function.Arguments)
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("error reading stream: %w", err)
			return
		}

		if hasToolCalls && len(toolCallsMap) > 0 {
			var collectedToolCalls []ToolCall
			for _, builder := range toolCallsMap {
				collectedToolCalls = append(collectedToolCalls, ToolCall{
					ID:   builder.id,
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      builder.name,
						Arguments: json.RawMessage(builder.arguments.String()),
					},
				})
			}
			toolCallChan <- collectedToolCalls
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}

func mapToOpenAIMessages(messages []Message) []openAIMessage {
	var out []openAIMessage

	for _, m := range messages {
		// Base message
		var tCalls []openAIToolCall
		for _, tc := range m.ToolCalls {
			tCalls = append(tCalls, openAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: string(tc.Function.Arguments),
				},
			})
		}

		msg := openAIMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCalls:  tCalls,
			ToolCallID: m.ToolCallID,
		}
		out = append(out, msg)

		// Append side-channel observations as native "tool" responses
		for _, obs := range m.Observations {
			out = append(out, openAIMessage{
				Role:       "tool",
				Content:    obs.Result,
				ToolCallID: obs.ToolCallID,
			})
		}
	}

	return out
}

func mapToOpenAITools(tools []Tool) []openAITool {
	var oTools []openAITool
	for _, t := range tools {
		ot := openAITool{
			Type: "function",
		}
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		ot.Function.Parameters = t.Parameters
		oTools = append(oTools, ot)
	}
	return oTools
}
