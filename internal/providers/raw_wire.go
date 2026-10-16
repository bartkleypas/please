package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/providers/wire"
)

// NormalizeOllamaGenerateEndpoint standardizes Ollama URLs for POST /api/generate.
func NormalizeOllamaGenerateEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "http://localhost:11434/api/generate"
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(endpoint, "/api/generate") {
		return endpoint
	}
	if strings.HasSuffix(endpoint, "/api/chat") {
		return strings.TrimSuffix(endpoint, "/chat") + "/generate"
	}
	if strings.HasSuffix(endpoint, "/api") {
		return endpoint + "/generate"
	}
	return endpoint + "/api/generate"
}

// RawWireProvider implements Provider by communicating directly with Ollama's
// POST /api/generate endpoint using "raw": true and streaming WireCodecs.
// This bypasses Ollama's chat templates and fragile middleware, giving Go direct
// control of the token stream, state machine, and multi-turn KV-cache prefix stability.
type RawWireProvider struct {
	Endpoint string
	Model    string
	Codec    wire.WireCodec
	Options  *domain.ModelOptions
	client   *http.Client
}

// NewRawWireProvider initializes a RawWireProvider instance with the given codec.
func NewRawWireProvider(endpoint, model string, codec wire.WireCodec, options *domain.ModelOptions) *RawWireProvider {
	if codec == nil {
		effort := "medium"
		if options != nil && options.ReasoningEffort != "" {
			effort = options.ReasoningEffort
		}
		codec = wire.NewGemma4Codec(wire.WithReasoningEffort(effort))
	} else if options != nil && options.ReasoningEffort != "" {
		if gc, ok := codec.(*wire.Gemma4Codec); ok {
			gc.ReasoningEffort = options.ReasoningEffort
		}
	}
	return &RawWireProvider{
		Endpoint: NormalizeOllamaGenerateEndpoint(endpoint),
		Model:    model,
		Codec:    codec,
		Options:  options,
		client:   &http.Client{},
	}
}

// SetClient overrides the internal HTTP client (used for hermetic unit testing and custom transports).
func (p *RawWireProvider) SetClient(client *http.Client) {
	p.client = client
}

func (p *RawWireProvider) buildOllamaOptions() map[string]interface{} {
	opts := make(map[string]interface{})
	if p.Options != nil {
		if p.Options.Temperature != nil {
			opts["temperature"] = *p.Options.Temperature
		}
		if p.Options.TopP != nil {
			opts["top_p"] = *p.Options.TopP
		}
		if p.Options.TopK != nil {
			opts["top_k"] = *p.Options.TopK
		}
		if p.Options.MinP != nil {
			opts["min_p"] = *p.Options.MinP
		}
		if p.Options.NumCtx != nil {
			opts["num_ctx"] = *p.Options.NumCtx
		}
		if p.Options.MaxTokens != nil {
			opts["num_predict"] = *p.Options.MaxTokens
		}
		// Note: Do NOT apply high repeat_penalty that would penalize structural control tokens (<|channel>, <channel|>, <|"|>)
		if p.Options.RepeatPenalty != nil {
			rp := *p.Options.RepeatPenalty
			if rp > 1.1 {
				rp = 1.05
			}
			opts["repeat_penalty"] = rp
		}
		if p.Options.RepeatLastN != nil {
			opts["repeat_last_n"] = *p.Options.RepeatLastN
		}
		if p.Options.FrequencyPenalty != nil {
			opts["frequency_penalty"] = *p.Options.FrequencyPenalty
		}
	}

	// Attach codec stop sequences
	if p.Codec != nil {
		opts["stop"] = p.Codec.StopSequences()
	}

	return opts
}

type ollamaRawGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Raw     bool                   `json:"raw"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type ollamaRawGenerateResponse struct {
	Model      string `json:"model"`
	CreatedAt  string `json:"created_at"`
	Response   string `json:"response"`
	Thinking   string `json:"thinking,omitempty"`
	Reasoning  string `json:"reasoning,omitempty"`
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason,omitempty"`
}

// GenerateResponse performs a synchronous request by executing the raw wire stream to completion.
func (p *RawWireProvider) GenerateResponse(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (*domain.Message, error) {
	contentChan, thoughtChan, toolCallsChan, errChan := p.GenerateResponseStream(ctx, messages, availableTools)

	var fullContent strings.Builder
	var fullThought strings.Builder
	var collectedToolCalls []domain.ToolCall

	for contentChan != nil || thoughtChan != nil || toolCallsChan != nil || errChan != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			} else if err != nil {
				return nil, err
			}

		case chunk, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
			} else if chunk != "" {
				fullThought.WriteString(chunk)
			}

		case chunk, ok := <-contentChan:
			if !ok {
				contentChan = nil
			} else if chunk != "" {
				fullContent.WriteString(chunk)
			}

		case calls, ok := <-toolCallsChan:
			if !ok {
				toolCallsChan = nil
			} else {
				collectedToolCalls = append(collectedToolCalls, calls...)
			}
		}
	}

	return &domain.Message{
		Role:      domain.RoleAssistant,
		Content:   fullContent.String(),
		Thought:   fullThought.String(),
		ToolCalls: collectedToolCalls,
	}, nil
}

// GenerateResponseStream performs a streaming request against Ollama's POST /api/generate endpoint,
// feeding raw tokens through the WireCodec's StreamLexer.
func (p *RawWireProvider) GenerateResponseStream(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (<-chan string, <-chan string, <-chan []domain.ToolCall, <-chan error) {
	contentChan := make(chan string)
	thoughtChan := make(chan string)
	toolCallChan := make(chan []domain.ToolCall, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(thoughtChan)
		defer close(toolCallChan)
		defer close(errChan)

		// Sync ReasoningEffort from Options to Codec if supported
		if p.Options != nil && p.Options.ReasoningEffort != "" {
			if gc, ok := p.Codec.(*wire.Gemma4Codec); ok {
				gc.ReasoningEffort = p.Options.ReasoningEffort
			}
		}

		// 1. Encode prompt using WireCodec
		prompt, err := p.Codec.EncodePrompt("", availableTools, messages)
		if err != nil {
			errChan <- fmt.Errorf("failed to encode raw wire prompt: %w", err)
			return
		}

		// 2. Build /api/generate request
		reqBody := ollamaRawGenerateRequest{
			Model:   p.Model,
			Prompt:  prompt,
			Raw:     true,
			Stream:  true,
			Options: p.buildOllamaOptions(),
		}

		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			errChan <- fmt.Errorf("failed to marshal generate request: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", p.Endpoint, bytes.NewBuffer(jsonBody))
		if err != nil {
			errChan <- fmt.Errorf("failed to create generate request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			errChan <- fmt.Errorf("raw wire request failed: %w", err)
			return
		}
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf("ollama returned non-ok status: %d, body: %s", resp.StatusCode, string(bodyBytes))
			return
		}

		// 3. Process NDJSON response stream with stateful StreamLexer
		lexer := p.Codec.NewStreamLexer()
		decoder := json.NewDecoder(resp.Body)
		var collectedToolCalls []domain.ToolCall

		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			var chunk ollamaRawGenerateResponse
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					break
				}
				errChan <- fmt.Errorf("failed to decode generate stream chunk: %w", err)
				return
			}

			if chunk.Thinking != "" {
				thoughtChan <- chunk.Thinking
			} else if chunk.Reasoning != "" {
				thoughtChan <- chunk.Reasoning
			}

			if chunk.Response != "" {
				events := lexer.Feed(chunk.Response)
				for _, ev := range events {
					switch ev.Kind {
					case wire.EventThoughtDelta:
						if ev.Text != "" {
							thoughtChan <- ev.Text
						}
					case wire.EventTextDelta:
						if ev.Text != "" {
							contentChan <- ev.Text
						}
					case wire.EventToolCall:
						if ev.ToolCall != nil {
							collectedToolCalls = append(collectedToolCalls, *ev.ToolCall)
						}
					}
				}
			}

			if chunk.Done {
				break
			}
		}

		// Flush any buffered tokens and evaluate end-of-stream fallbacks
		closeEvents, err := lexer.Close()
		if err != nil {
			errChan <- fmt.Errorf("lexer close error: %w", err)
			return
		}

		for _, ev := range closeEvents {
			switch ev.Kind {
			case wire.EventThoughtDelta:
				if ev.Text != "" {
					thoughtChan <- ev.Text
				}
			case wire.EventTextDelta:
				if ev.Text != "" {
					contentChan <- ev.Text
				}
			case wire.EventToolCall:
				if ev.ToolCall != nil {
					collectedToolCalls = append(collectedToolCalls, *ev.ToolCall)
				}
			}
		}

		if len(collectedToolCalls) > 0 {
			toolCallChan <- collectedToolCalls
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}
