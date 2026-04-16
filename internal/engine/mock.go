package engine

import (
	"context"
	"time"
)

// MockLLMProvider allows us to control the LLM response in tests
type MockLLMProvider struct {
	ResponseContent string
	ResponseErr     error
	Delay           time.Duration
}

// GenerateResponse implements the LLMProvider interface for testing purposes
func (m *MockLLMProvider) GenerateResponse(ctx context.Context, messages []Message) (string, error) {
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	if m.ResponseErr != nil {
		return "", m.ResponseErr
	}

	return m.ResponseContent, nil
}

// GenerateResponseStream implements the LLMProvider interface for testing purposes
func (m *MockLLMProvider) GenerateResponseStream(ctx context.Context, messages []Message) (<-chan string, <-chan error) {
	contentChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(errChan)

		if m.Delay > 0 {
			select {
			case <-time.After(m.Delay):
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}

		if m.ResponseErr != nil {
			errChan <- m.ResponseErr
			return
		}

		// Send content in a single chunk for simplicity in mock
		contentChan <- m.ResponseContent
	}()

	return contentChan, errChan
}
