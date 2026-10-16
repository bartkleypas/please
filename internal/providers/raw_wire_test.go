package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/providers/wire"
)

type mockRoundTripper func(req *http.Request) (*http.Response, error)

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m(req)
}

func TestNormalizeOllamaGenerateEndpoint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "http://localhost:11434/api/generate"},
		{"localhost:11434", "http://localhost:11434/api/generate"},
		{"http://localhost:11434/api/chat", "http://localhost:11434/api/generate"},
		{"http://localhost:11434/api", "http://localhost:11434/api/generate"},
		{"http://localhost:11434/api/generate", "http://localhost:11434/api/generate"},
		{"https://ollama.internal:8443", "https://ollama.internal:8443/api/generate"},
	}

	for _, tt := range tests {
		got := NormalizeOllamaGenerateEndpoint(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeOllamaGenerateEndpoint(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestRawWireProvider_OptionsAndPayload(t *testing.T) {
	repeatPen := 1.3 // Should be clamped
	temp := 0.7
	opts := &domain.ModelOptions{
		RepeatPenalty: &repeatPen,
		Temperature:   &temp,
	}

	codec := wire.NewGemma4Codec()
	provider := NewRawWireProvider("http://localhost:11434", "gemma4:e4b", codec, opts)

	var capturedReq ollamaRawGenerateRequest

	mockClient := &http.Client{
		Transport: mockRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/generate" {
				t.Errorf("expected path /api/generate, got %s", req.URL.Path)
			}
			bodyBytes, _ := io.ReadAll(req.Body)
			if err := json.Unmarshal(bodyBytes, &capturedReq); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			// Return NDJSON response stream
			ndjson := `{"model":"gemma4:e4b","response":"<|channel>thought\nThinking\n<channel|>","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":"Hello world!","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":"","done":true}` + "\n"

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(ndjson)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	provider.SetClient(mockClient)

	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "Say hi"},
	}

	resp, err := provider.GenerateResponse(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}

	// Verify request payload fields
	if !capturedReq.Raw {
		t.Errorf("expected raw: true, got false")
	}
	if !capturedReq.Stream {
		t.Errorf("expected stream: true, got false")
	}
	if capturedReq.Model != "gemma4:e4b" {
		t.Errorf("expected model gemma4:e4b, got %s", capturedReq.Model)
	}
	if !strings.HasSuffix(capturedReq.Prompt, "<|turn>model\n") {
		t.Errorf("expected prompt to end with <|turn>model\\n, got:\n%s", capturedReq.Prompt)
	}

	// Verify repeat_penalty clamped
	rp, ok := capturedReq.Options["repeat_penalty"].(float64)
	if !ok || rp > 1.1 {
		t.Errorf("expected repeat_penalty to be clamped <= 1.1, got %v", capturedReq.Options["repeat_penalty"])
	}

	// Verify stop sequences passed
	stops, ok := capturedReq.Options["stop"].([]interface{})
	if !ok || len(stops) != 2 {
		t.Errorf("expected 2 stop sequences in options.stop, got %v", capturedReq.Options["stop"])
	}

	// Verify parsed response
	if resp.Thought != "Thinking\n" {
		t.Errorf("expected thought 'Thinking\\n', got %q", resp.Thought)
	}
	if resp.Content != "Hello world!" {
		t.Errorf("expected content 'Hello world!', got %q", resp.Content)
	}
}

func TestRawWireProvider_StreamToolCalls(t *testing.T) {
	codec := wire.NewGemma4Codec()
	provider := NewRawWireProvider("http://localhost:11434", "gemma4:e4b", codec, nil)

	mockClient := &http.Client{
		Transport: mockRoundTripper(func(req *http.Request) (*http.Response, error) {
			ndjson := `{"model":"gemma4:e4b","response":"<|channel>thought\nI will list files.\n<channel|>","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":"<|tool_call>call:list_dir{path:<|\"|>","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":".<|\"|>}<tool_call|>","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":"","done":true}` + "\n"

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(ndjson)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	provider.SetClient(mockClient)

	tools := []domain.ToolSpec{
		{
			Name:        "list_dir",
			Description: "Lists files",
			Parameters:  map[string]interface{}{"path": "string"},
		},
	}

	contentChan, thoughtChan, toolCallsChan, errChan := provider.GenerateResponseStream(
		context.Background(),
		[]domain.Message{{Role: domain.RoleUser, Content: "List directory"}},
		tools,
	)

	var thoughts []string
	var toolCalls []domain.ToolCall

	for contentChan != nil || thoughtChan != nil || toolCallsChan != nil || errChan != nil {
		select {
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		case th, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
			} else {
				thoughts = append(thoughts, th)
			}
		case _, ok := <-contentChan:
			if !ok {
				contentChan = nil
			}
		case tc, ok := <-toolCallsChan:
			if !ok {
				toolCallsChan = nil
			} else {
				toolCalls = append(toolCalls, tc...)
			}
		}
	}

	fullThought := strings.Join(thoughts, "")
	if fullThought != "I will list files.\n" {
		t.Errorf("unexpected thought: %q", fullThought)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "list_dir" {
		t.Errorf("expected list_dir, got %s", toolCalls[0].Function.Name)
	}
	var args map[string]interface{}
	_ = json.Unmarshal(toolCalls[0].Function.Arguments, &args)
	if args["path"] != "." {
		t.Errorf("expected path '.', got %v", args["path"])
	}
}

func TestRawWireProvider_NativeThinkingFields(t *testing.T) {
	codec := wire.NewGemma4Codec()
	provider := NewRawWireProvider("http://localhost:11434", "gemma4:e4b", codec, nil)

	mockClient := &http.Client{
		Transport: mockRoundTripper(func(req *http.Request) (*http.Response, error) {
			ndjson := `{"model":"gemma4:e4b","thinking":"I am thinking via native field.\n","response":"","done":false}` + "\n" +
				`{"model":"gemma4:e4b","reasoning":"More reasoning.\n","response":"","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":"Here is output.","done":false}` + "\n" +
				`{"model":"gemma4:e4b","response":"","done":true}` + "\n"

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(ndjson)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	provider.SetClient(mockClient)

	contentChan, thoughtChan, _, errChan := provider.GenerateResponseStream(
		context.Background(),
		[]domain.Message{{Role: domain.RoleUser, Content: "Hello"}},
		nil,
	)

	var thoughts []string
	var contents []string

	for contentChan != nil || thoughtChan != nil || errChan != nil {
		select {
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		case th, ok := <-thoughtChan:
			if !ok {
				thoughtChan = nil
			} else {
				thoughts = append(thoughts, th)
			}
		case c, ok := <-contentChan:
			if !ok {
				contentChan = nil
			} else {
				contents = append(contents, c)
			}
		}
	}

	fullThought := strings.Join(thoughts, "")
	expectedThought := "I am thinking via native field.\nMore reasoning.\n"
	if fullThought != expectedThought {
		t.Errorf("expected thought %q, got %q", expectedThought, fullThought)
	}

	fullContent := strings.Join(contents, "")
	if fullContent != "Here is output." {
		t.Errorf("expected content 'Here is output.', got %q", fullContent)
	}
}

