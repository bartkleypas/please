package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Message represents a simplified message for LLM providers
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Thought    string     `json:"thought,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
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
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	Stream   bool         `json:"stream"`
	Tools    []ollamaTool `json:"tools,omitempty"`
}

type ollamaResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
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
		Messages: prepareOllamaMessages(messages),
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

	msg := &ollamaResp.Message
	var thoughts []string
	var content strings.Builder

	curr := msg.Content
	for {
		startIdx := strings.Index(curr, "<thought>")
		if startIdx == -1 {
			content.WriteString(curr)
			break
		}
		content.WriteString(curr[:startIdx])
		curr = curr[startIdx+9:]

		endIdx := strings.Index(curr, "</thought>")
		if endIdx == -1 {
			thoughts = append(thoughts, curr)
			break
		}
		thoughts = append(thoughts, curr[:endIdx])
		curr = curr[endIdx+10:]
	}

	msg.Thought = strings.Join(thoughts, "\n\n")
	msg.Content = strings.TrimSpace(content.String())

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
			Messages: prepareOllamaMessages(messages),
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
		
		splitter := &streamSplitter{
			thoughtChan: thoughtChan,
			contentChan: contentChan,
		}

		for {
			var ollamaResp ollamaResponse
			if err := decoder.Decode(&ollamaResp); err != nil {
				if err == io.EOF {
					break
				}
				errChan <- fmt.Errorf("failed to decode streaming response: %w", err)
				return
			}

			// Send content if present through splitter
			if ollamaResp.Message.Content != "" {
				splitter.process(ollamaResp.Message.Content)
			}

			// Collect tool calls if present
			if len(ollamaResp.Message.ToolCalls) > 0 {
				collectedToolCalls = append(collectedToolCalls, ollamaResp.Message.ToolCalls...)
			}

			if ollamaResp.Done {
				break
			}
		}
		
		splitter.flush()

		if len(collectedToolCalls) > 0 {
			toolCallChan <- collectedToolCalls
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}

type streamSplitter struct {
	thoughtChan chan<- string
	contentChan chan<- string
	isThinking  bool
	buffer      string
}

func (s *streamSplitter) process(chunk string) {
	s.buffer += chunk
	for {
		if !s.isThinking {
			idx := strings.Index(s.buffer, "<thought>")
			if idx == -1 {
				if len(s.buffer) > 9 {
					s.contentChan <- s.buffer[:len(s.buffer)-9]
					s.buffer = s.buffer[len(s.buffer)-9:]
				}
				return
			}
			s.contentChan <- s.buffer[:idx]
			s.isThinking = true
			s.buffer = s.buffer[idx+9:]
		} else {
			idx := strings.Index(s.buffer, "</thought>")
			if idx == -1 {
				if len(s.buffer) > 10 {
					s.thoughtChan <- s.buffer[:len(s.buffer)-10]
					s.buffer = s.buffer[len(s.buffer)-10:]
				}
				return
			}
			s.thoughtChan <- s.buffer[:idx]
			s.isThinking = false
			s.buffer = s.buffer[idx+10:]
		}
	}
}

func (s *streamSplitter) flush() {
	if s.buffer == "" {
		return
	}
	if s.isThinking {
		s.thoughtChan <- s.buffer
	} else {
		s.contentChan <- s.buffer
	}
	s.buffer = ""
}

// prepareOllamaMessages re-injects thoughts into the content field for the Ollama API,
// ensuring the model maintains its internal monologue across tool-call boundaries.
func prepareOllamaMessages(messages []Message) []Message {
	prepared := make([]Message, len(messages))
	for i, m := range messages {
		prepared[i] = m
		if m.Thought != "" {
			prepared[i].Content = "<thought>\n" + m.Thought + "\n</thought>\n\n" + m.Content
		}
	}
	return prepared
}
