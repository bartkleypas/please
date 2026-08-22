package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

type OpenAIProvider struct {
	Endpoint string
	Model    string
	APIKey   string
	Options  *ModelOptions
	client   *http.Client
}

func NewOpenAIProvider(endpoint, model, apiKey string, options *ModelOptions) *OpenAIProvider {
	return &OpenAIProvider{
		Endpoint: endpoint,
		Model:    model,
		APIKey:   apiKey,
		Options:  options,
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
	Role             string           `json:"role"`
	Content          interface{}      `json:"content"` // Can be string or []openAIContentPart
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Name             string           `json:"name,omitempty"`
	ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type openAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

type openAIToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Tools       []openAITool    `json:"tools,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
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
	if o.Options != nil {
		reqBody.Temperature = o.Options.Temperature
		reqBody.TopP = o.Options.TopP
		reqBody.MaxTokens = o.Options.MaxTokens
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

	contentStr, _ := choice.Content.(string)
	msg := &Message{
		Role:      RoleAssistant,
		Content:   contentStr,
		ToolCalls: tCalls,
	}

	if choice.ReasoningContent != "" {
		msg.Thought = choice.ReasoningContent
	} else if choice.Reasoning != "" {
		msg.Thought = choice.Reasoning
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
		if o.Options != nil {
			reqBody.Temperature = o.Options.Temperature
			reqBody.TopP = o.Options.TopP
			reqBody.MaxTokens = o.Options.MaxTokens
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

			if delta.ReasoningContent != "" {
				thoughtChan <- delta.ReasoningContent
			} else if delta.Reasoning != "" {
				thoughtChan <- delta.Reasoning
			}

			if contentStr, ok := delta.Content.(string); ok && contentStr != "" {
				contentChan <- contentStr
			}

			if len(delta.ToolCalls) > 0 {
				hasToolCalls = true
				for _, tc := range delta.ToolCalls {
					if tc.Index == nil {
						continue // Malformed tool call delta without an index
					}

					idx := *tc.Index
					if tc.ID != "" {
						toolCallsMap[idx] = &toolCallBuilder{
							id:   tc.ID,
							name: tc.Function.Name,
						}
					}
					if tc.Function.Arguments != "" && toolCallsMap[idx] != nil {
						toolCallsMap[idx].arguments.WriteString(tc.Function.Arguments)
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

		var openAIContent interface{} = m.Content
		if len(m.Images) > 0 {
			var parts []openAIContentPart
			if m.Content != "" {
				parts = append(parts, openAIContentPart{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, imgPath := range m.Images {
				b64, err := encodeImageToBase64(imgPath)
				if err == nil {
					mimeType := "image/png"
					switch strings.ToLower(filepath.Ext(imgPath)) {
					case ".jpg", ".jpeg":
						mimeType = "image/jpeg"
					case ".gif":
						mimeType = "image/gif"
					case ".webp":
						mimeType = "image/webp"
					}
					parts = append(parts, openAIContentPart{
						Type: "image_url",
						ImageURL: &openAIImageURL{
							URL: fmt.Sprintf("data:%s;base64,%s", mimeType, b64),
						},
					})
				} else {
					fmt.Printf("Warning: failed to read image for openai payload: %v\n", err)
				}
			}
			openAIContent = parts
		}

		roleStr := string(m.Role)
		if m.Role == RoleSummary {
			roleStr = "system"
			if strContent, ok := openAIContent.(string); ok {
				openAIContent = "[Conversation Milestone & Summary Context]:\n" + strContent
			}
		}

		msg := openAIMessage{
			Role:       roleStr,
			Content:    openAIContent,
			ToolCalls:  tCalls,
			ToolCallID: m.ToolCallID,
		}
		out = append(out, msg)
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
