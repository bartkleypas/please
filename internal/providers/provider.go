package providers

import (
	"context"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/tools"
)

// Provider defines the interface for interacting with different AI model backends.
type Provider interface {
	GenerateResponse(ctx context.Context, messages []domain.Message, availableTools []tools.Tool) (*domain.Message, error)
	GenerateResponseStream(ctx context.Context, messages []domain.Message, availableTools []tools.Tool) (<-chan string, <-chan string, <-chan []domain.ToolCall, <-chan error)
}

// LLMProvider is an alias for Provider for backward compatibility.
type LLMProvider = Provider
