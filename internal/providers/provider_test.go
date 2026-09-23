package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/domain"
)

func TestMockLLMProvider_Sync(t *testing.T) {
	mock := &MockLLMProvider{
		ResponseContent: "Hello from mock!",
		ResponseThought: "Mock thought",
		ResponseToolCalls: []domain.ToolCall{
			{
				ID:   "call_mock",
				Type: "function",
			},
		},
	}

	msg, err := mock.GenerateResponse(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if msg.Content != "Hello from mock!" {
		t.Errorf("expected 'Hello from mock!', got '%s'", msg.Content)
	}
	if msg.Thought != "Mock thought" {
		t.Errorf("expected 'Mock thought', got '%s'", msg.Thought)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_mock" {
		t.Errorf("unexpected tool calls: %+v", msg.ToolCalls)
	}
}

func TestMockLLMProvider_Stream(t *testing.T) {
	mock := &MockLLMProvider{
		StreamHandler: func(messages []domain.Message, availableTools []domain.ToolSpec) (string, string, []domain.ToolCall, error) {
			return "Streamed text", "Streamed thought", nil, nil
		},
	}

	contentChan, thoughtChan, _, errChan := mock.GenerateResponseStream(context.Background(), nil, nil)

	var content string
	var thought string
	for contentChan != nil || thoughtChan != nil {
		select {
		case c, ok := <-contentChan:
			if !ok {
				contentChan = nil
				continue
			}
			content += c
		case th, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
				continue
			}
			thought += th
		case err := <-errChan:
			if err != nil {
				t.Fatalf("stream error: %v", err)
			}
		}
	}

	if content != "Streamed text" {
		t.Errorf("expected 'Streamed text', got '%s'", content)
	}
	if thought != "Streamed thought" {
		t.Errorf("expected 'Streamed thought', got '%s'", thought)
	}
}

func TestOllamaProvider_OptionsMapping(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 40
	minP := 0.05
	numCtx := 8192
	maxTokens := 2048
	repeatPenalty := 1.1
	repeatLastN := 64
	freqPenalty := 0.2

	opts := &domain.ModelOptions{
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topK,
		MinP:             &minP,
		NumCtx:           &numCtx,
		MaxTokens:        &maxTokens,
		RepeatPenalty:    &repeatPenalty,
		RepeatLastN:      &repeatLastN,
		FrequencyPenalty: &freqPenalty,
	}

	provider := NewOllamaProvider("http://localhost:11434/api/chat", "test-model", opts)
	built := provider.buildOllamaOptions()

	if built["temperature"] != 0.7 {
		t.Errorf("expected temp 0.7, got %v", built["temperature"])
	}
	if built["top_p"] != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", built["top_p"])
	}
	if built["num_predict"] != 2048 {
		t.Errorf("expected num_predict 2048, got %v", built["num_predict"])
	}
	if built["num_ctx"] != 8192 {
		t.Errorf("expected num_ctx 8192, got %v", built["num_ctx"])
	}
}

func TestOllamaProvider_OptionsSerialization(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		resp := ollamaResponse{
			Message: ollamaMessage{
				Role:    "assistant",
				Content: "Hello from mock ollama",
			},
			Done: true,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	temp := 0.7
	topP := 0.9
	topK := 40
	minP := 0.05
	numCtx := 16384
	maxTokens := 2048
	repeatPenalty := 1.1
	repeatLastN := 128
	freqPenalty := 0.15

	options := &domain.ModelOptions{
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topK,
		MinP:             &minP,
		NumCtx:           &numCtx,
		MaxTokens:        &maxTokens,
		RepeatPenalty:    &repeatPenalty,
		RepeatLastN:      &repeatLastN,
		FrequencyPenalty: &freqPenalty,
	}

	provider := NewOllamaProvider(server.URL, "test-model", options)
	ctx := context.Background()
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if resp.Content != "Hello from mock ollama" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal(capturedBody, &reqData); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	opts, ok := reqData["options"].(map[string]interface{})
	if !ok || opts == nil {
		t.Fatalf("expected options object in request body, got: %v", reqData["options"])
	}

	if opts["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", opts["temperature"])
	}
	if opts["top_p"] != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", opts["top_p"])
	}
	if opts["top_k"] != float64(40) {
		t.Errorf("expected top_k 40, got %v", opts["top_k"])
	}
	if opts["min_p"] != 0.05 {
		t.Errorf("expected min_p 0.05, got %v", opts["min_p"])
	}
	if opts["num_ctx"] != float64(16384) {
		t.Errorf("expected num_ctx 16384, got %v", opts["num_ctx"])
	}
	if opts["num_predict"] != float64(2048) {
		t.Errorf("expected num_predict 2048, got %v", opts["num_predict"])
	}
	if opts["repeat_penalty"] != 1.1 {
		t.Errorf("expected repeat_penalty 1.1, got %v", opts["repeat_penalty"])
	}
	if opts["repeat_last_n"] != float64(128) {
		t.Errorf("expected repeat_last_n 128, got %v", opts["repeat_last_n"])
	}
	if opts["frequency_penalty"] != 0.15 {
		t.Errorf("expected frequency_penalty 0.15, got %v", opts["frequency_penalty"])
	}
}

func TestOpenAIProvider_GenerateResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := openAIResponse{
			Choices: []struct {
				Message      openAIMessage `json:"message"`
				Delta        openAIMessage `json:"delta"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message: openAIMessage{
						Role:             "assistant",
						Content:          "OpenAI answer",
						ReasoningContent: "Reasoning trace",
					},
					FinishReason: "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewOpenAIProvider(ts.URL, "gpt-test", "test-key", nil)
	msg, err := provider.GenerateResponse(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "test"}}, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}

	if msg.Content != "OpenAI answer" {
		t.Errorf("expected 'OpenAI answer', got '%s'", msg.Content)
	}
	if msg.Thought != "Reasoning trace" {
		t.Errorf("expected 'Reasoning trace', got '%s'", msg.Thought)
	}
}

func TestOpenAIProvider_OptionsSerialization(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		resp := openAIResponse{
			Choices: []struct {
				Message      openAIMessage `json:"message"`
				Delta        openAIMessage `json:"delta"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message: openAIMessage{
						Role:    "assistant",
						Content: "Hello from mock openai",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	temp := 0.2
	topP := 0.85
	maxTokens := 1024
	freqPenalty := 0.25

	options := &domain.ModelOptions{
		Temperature:      &temp,
		TopP:             &topP,
		MaxTokens:        &maxTokens,
		FrequencyPenalty: &freqPenalty,
	}

	provider := NewOpenAIProvider(server.URL, "gpt-4o", "test-key", options)
	ctx := context.Background()
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if resp.Content != "Hello from mock openai" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal(capturedBody, &reqData); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	if reqData["temperature"] != 0.2 {
		t.Errorf("expected temperature 0.2, got %v", reqData["temperature"])
	}
	if reqData["top_p"] != 0.85 {
		t.Errorf("expected top_p 0.85, got %v", reqData["top_p"])
	}
	if reqData["max_tokens"] != float64(1024) {
		t.Errorf("expected max_tokens 1024, got %v", reqData["max_tokens"])
	}
	if reqData["frequency_penalty"] != 0.25 {
		t.Errorf("expected frequency_penalty 0.25, got %v", reqData["frequency_penalty"])
	}
}

func TestOpenAIProvider_ReasoningExtraction(t *testing.T) {
	// 1. Test Batch mode with reasoning_content
	serverBatch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := openAIResponse{
			Choices: []struct {
				Message      openAIMessage `json:"message"`
				Delta        openAIMessage `json:"delta"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message: openAIMessage{
						Role:             "assistant",
						Content:          "Final answer",
						ReasoningContent: "Thinking step 1... step 2...",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer serverBatch.Close()

	providerBatch := NewOpenAIProvider(serverBatch.URL, "deepseek-r1", "key", nil)
	msg, err := providerBatch.GenerateResponse(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "problem"}}, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if msg.Content != "Final answer" {
		t.Errorf("expected content 'Final answer', got '%s'", msg.Content)
	}
	if msg.Thought != "Thinking step 1... step 2..." {
		t.Errorf("expected thought 'Thinking step 1... step 2...', got '%s'", msg.Thought)
	}

	// 2. Test Streaming mode with reasoning_content deltas
	serverStream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Thought chunk \"}}]}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Answer chunk\"}}]}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer serverStream.Close()

	providerStream := NewOpenAIProvider(serverStream.URL, "deepseek-r1", "key", nil)
	contentChan, thoughtChan, _, errChan := providerStream.GenerateResponseStream(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "problem"}}, nil)

	var receivedContent string
	var receivedThought string

	for {
		select {
		case c, ok := <-contentChan:
			if ok {
				receivedContent += c
			}
		case th, ok := <-thoughtChan:
			if ok {
				receivedThought += th
			}
		case err, ok := <-errChan:
			if ok && err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
			goto Done
		}
		if contentChan == nil && thoughtChan == nil {
			break
		}
	}
Done:
	if receivedThought != "Thought chunk " {
		t.Errorf("expected stream thought 'Thought chunk ', got '%s'", receivedThought)
	}
	if receivedContent != "Answer chunk" {
		t.Errorf("expected stream content 'Answer chunk', got '%s'", receivedContent)
	}
}

func TestMapToOpenAIMessages_SummaryRole(t *testing.T) {
	msgs := []domain.Message{
		{
			Role:    domain.RoleSummary,
			Content: "Previous discussion on architecture.",
		},
	}
	mapped := mapToOpenAIMessages(msgs)
	if len(mapped) != 1 {
		t.Fatalf("expected 1 mapped message, got %d", len(mapped))
	}
	if mapped[0].Role != "system" {
		t.Errorf("expected RoleSummary to map to 'system', got '%s'", mapped[0].Role)
	}
	contentStr, ok := mapped[0].Content.(string)
	if !ok || !strings.Contains(contentStr, "[Conversation Milestone & Summary Context]") {
		t.Errorf("expected milestone header in summary content, got '%v'", mapped[0].Content)
	}
}

func TestNormalizeOllamaEndpoint(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", "http://localhost:11434/api/chat"},
		{"http://localhost:11434", "http://localhost:11434/api/chat"},
		{"http://localhost:11434/", "http://localhost:11434/api/chat"},
		{"http://localhost:11434/api", "http://localhost:11434/api/chat"},
		{"http://localhost:11434/api/", "http://localhost:11434/api/chat"},
		{"http://localhost:11434/api/chat", "http://localhost:11434/api/chat"},
		{"localhost:11434", "http://localhost:11434/api/chat"},
		{"https://ollama.internal.net", "https://ollama.internal.net/api/chat"},
	}

	for _, tc := range cases {
		got := NormalizeOllamaEndpoint(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeOllamaEndpoint(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestNormalizeOpenAIEndpoint(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"http://localhost:1234/v1", "http://localhost:1234/v1/chat/completions"},
		{"localhost:11434/v1", "http://localhost:11434/v1/chat/completions"},
		{"127.0.0.1:8000/v1", "http://127.0.0.1:8000/v1/chat/completions"},
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api/v1/chat/completions"},
	}

	for _, tc := range cases {
		got := NormalizeOpenAIEndpoint(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeOpenAIEndpoint(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}
