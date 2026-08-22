package engine

import (
	"context"
	"time"
)

// MockLLMProvider allows us to control the LLM response in tests
type MockLLMProvider struct {
	ResponseContent   string
	ResponseThought   string
	ResponseToolCalls []ToolCall
	ResponseErr       error
	Delay             time.Duration
	StreamHandler     func(messages []Message, tools []Tool) (string, string, []ToolCall, error)
}

// GenerateResponse implements the LLMProvider interface for testing purposes
func (m *MockLLMProvider) GenerateResponse(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.StreamHandler != nil {
		content, thought, tCalls, err := m.StreamHandler(messages, tools)
		if err != nil {
			return nil, err
		}
		return &Message{
			Role:      RoleAssistant,
			Content:   content,
			Thought:   thought,
			ToolCalls: tCalls,
		}, nil
	}

	if m.ResponseErr != nil {
		return nil, m.ResponseErr
	}

	return &Message{
		Role:      RoleAssistant,
		Content:   m.ResponseContent,
		Thought:   m.ResponseThought,
		ToolCalls: m.ResponseToolCalls,
	}, nil
}

// GenerateResponseStream implements the LLMProvider interface for testing purposes
func (m *MockLLMProvider) GenerateResponseStream(ctx context.Context, messages []Message, tools []Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error) {
	contentChan := make(chan string)
	thoughtChan := make(chan string)
	toolCallChan := make(chan []ToolCall, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(thoughtChan)
		defer close(toolCallChan)
		defer close(errChan)

		if m.Delay > 0 {
			select {
			case <-time.After(m.Delay):
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}

		if m.StreamHandler != nil {
			content, thought, tCalls, err := m.StreamHandler(messages, tools)
			if err != nil {
				errChan <- err
				return
			}
			if thought != "" {
				thoughtChan <- thought
			}
			if len(tCalls) > 0 {
				toolCallChan <- tCalls
			}
			if content != "" {
				contentChan <- content
			}
			return
		}

		if m.ResponseErr != nil {
			errChan <- m.ResponseErr
			return
		}

		if m.ResponseThought != "" {
			thoughtChan <- m.ResponseThought
		}
		if len(m.ResponseToolCalls) > 0 {
			toolCallChan <- m.ResponseToolCalls
		}
		if m.ResponseContent != "" {
			contentChan <- m.ResponseContent
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}
