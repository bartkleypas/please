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
