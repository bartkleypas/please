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
	var toolCalls []ToolCall

	curr := msg.Content
	for {
		tIdx := strings.Index(curr, "<thought>")
		cIdx := strings.Index(curr, "<tool_call>")

		if tIdx == -1 && cIdx == -1 {
			content.WriteString(curr)
			break
		}

		if tIdx != -1 && (cIdx == -1 || tIdx < cIdx) {
			content.WriteString(curr[:tIdx])
			curr = curr[tIdx+9:]
			endIdx := strings.Index(curr, "</thought>")
			if endIdx == -1 {
				thoughts = append(thoughts, curr)
				curr = ""
				break
			}
			thoughts = append(thoughts, curr[:endIdx])
			curr = curr[endIdx+10:]
		} else {
			content.WriteString(curr[:cIdx])
			curr = curr[cIdx+11:]
			endIdx := strings.Index(curr, "</tool_call>")
			if endIdx == -1 {
				curr = ""
				break
			}
			callStr := curr[:endIdx]
			
			var call ToolCall
			if err := json.Unmarshal([]byte(callStr), &call); err != nil || call.Function.Name == "" {
				var flat struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if errFlat := json.Unmarshal([]byte(callStr), &flat); errFlat == nil && flat.Name != "" {
					call.Function.Name = flat.Name
					call.Function.Arguments = flat.Arguments
					call.Type = "function"
				}
			}
			if call.Function.Name != "" {
				toolCalls = append(toolCalls, call)
			}
			curr = curr[endIdx+12:]
		}
	}

	msg.Thought = strings.Join(thoughts, "\n\n")
	msg.Content = strings.TrimSpace(content.String())
	msg.ToolCalls = toolCalls

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
			toolChan:    toolCallChan,
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
	toolChan    chan<- []ToolCall
	isThinking  bool
	hasThought  bool
	isCalling   bool
	buffer      string
}

func (s *streamSplitter) process(chunk string) {
	s.buffer += chunk
	for {
		if !s.isThinking && !s.isCalling {
			// Check for thought start
			tIdx := strings.Index(s.buffer, "<thought>")
			// Check for tool call start
			cIdx := strings.Index(s.buffer, "<tool_call>")

			if tIdx == -1 && cIdx == -1 {
				// No tags, send safe content
				if len(s.buffer) > 11 { // Length of longest tag
					s.contentChan <- s.buffer[:len(s.buffer)-11]
					s.buffer = s.buffer[len(s.buffer)-11:]
				}
				return
			}

			// Determine which tag comes first
			if tIdx != -1 && (cIdx == -1 || tIdx < cIdx) {
				s.contentChan <- s.buffer[:tIdx]
				s.isThinking = true
				s.buffer = s.buffer[tIdx+9:]
			} else {
				s.contentChan <- s.buffer[:cIdx]
				s.isCalling = true
				s.buffer = s.buffer[cIdx+11:]
			}
		} else if s.isThinking {
			idx := strings.Index(s.buffer, "</thought>")
			if idx == -1 {
				if len(s.buffer) > 10 {
					s.thoughtChan <- s.buffer[:len(s.buffer)-10]
					s.buffer = s.buffer[len(s.buffer)-10:]
				}
				return
			}
			thought := s.buffer[:idx]
			s.thoughtChan <- thought
			if strings.TrimSpace(thought) != "" {
				s.hasThought = true
			}
			s.isThinking = false
			s.buffer = s.buffer[idx+10:]
		} else if s.isCalling {
			idx := strings.Index(s.buffer, "</tool_call>")
			if idx == -1 {
				return // Wait for end of tag
			}
			callStr := s.buffer[:idx]
			
			// Attempt to parse tool call flexibly
			var call ToolCall
			// 1. Try official nested format
			err := json.Unmarshal([]byte(callStr), &call)
			
			// 2. Try flattened format if nested fails or is empty
			if err != nil || call.Function.Name == "" {
				var flat struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if errFlat := json.Unmarshal([]byte(callStr), &flat); errFlat == nil && flat.Name != "" {
					call.Function.Name = flat.Name
					call.Function.Arguments = flat.Arguments
					if call.Type == "" {
						call.Type = "function"
					}
				}
			}

			if call.Function.Name != "" {
				s.toolChan <- []ToolCall{call}
			}
			
			s.isCalling = false
			s.buffer = s.buffer[idx+12:]
		}
	}
}

func (s *streamSplitter) flush() {
	if s.buffer == "" {
		return
	}
	if s.isThinking {
		s.thoughtChan <- s.buffer
		if strings.TrimSpace(s.buffer) != "" {
			s.hasThought = true
		}
	} else if s.isCalling {
		var call ToolCall
		err := json.Unmarshal([]byte(s.buffer), &call)
		if err != nil || call.Function.Name == "" {
			var flat struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if errFlat := json.Unmarshal([]byte(s.buffer), &flat); errFlat == nil && flat.Name != "" {
				call.Function.Name = flat.Name
				call.Function.Arguments = flat.Arguments
				call.Type = "function"
			}
		}
		if call.Function.Name != "" {
			s.toolChan <- []ToolCall{call}
		}
	} else {
		s.contentChan <- s.buffer
	}
	s.buffer = ""
}

// prepareOllamaMessages re-injects thoughts and side-channel observations into the content field for the Ollama API,
// ensuring the model maintains its internal monologue and reacts to tool results within the same turn context.
func prepareOllamaMessages(messages []Message) []Message {
	prepared := make([]Message, len(messages))
	for i, m := range messages {
		prepared[i] = m

		content := m.Content
		if m.Internal && m.Role == RoleAssistant {
			content = "<thought>\n" + m.Content + "\n</thought>"
		} else if m.Thought != "" {
			// Support for structural thoughts
			content = "<thought>\n" + m.Thought + "\n</thought>\n\n" + m.Content
		}

		// Inject observations (Side-Channel Interleaving)
		if len(m.Observations) > 0 {
			var obsBuilder strings.Builder
			obsBuilder.WriteString("<thought>\n")
			if m.Thought != "" {
				obsBuilder.WriteString(m.Thought)
				obsBuilder.WriteString("\n")
			}
			for _, obs := range m.Observations {
				// Reconstruct the tool call and result as a single thinking block
				// This tricks the model into thinking it just saw the result of its own call.
				fmt.Fprintf(&obsBuilder, "Observation (Call %s): %s\n", obs.ToolCallID, obs.Result)
			}
			obsBuilder.WriteString("Observation received. Complete your thought process.\n")
			obsBuilder.WriteString("</thought>\n\n")
			content = obsBuilder.String() + m.Content
		}

		prepared[i].Content = content
	}
	return prepared
}
