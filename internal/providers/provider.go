package providers

import (
	"context"

	"github.com/bartkleypas/please/internal/domain"
)

// Provider defines the interface for interacting with different AI model backends.
type Provider interface {
	GenerateResponse(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (*domain.Message, error)
	GenerateResponseStream(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (<-chan string, <-chan string, <-chan []domain.ToolCall, <-chan error)
}

// LLMProvider is an alias for Provider for backward compatibility.
type LLMProvider = Provider
