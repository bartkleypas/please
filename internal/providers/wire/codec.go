package wire

import (
	"github.com/bartkleypas/please/internal/domain"
)

// ToolDefinition represents the schema definition required by LLMs for function calling.
type ToolDefinition = domain.ToolDefinition

// TurnNode represents an individual turn or prompt segment prepared for an LLM provider.
type TurnNode = domain.TurnNode

// StreamEventKind indicates the semantic type of a lexed streaming chunk.
type StreamEventKind int

const (
	EventThoughtDelta StreamEventKind = iota
	EventTextDelta
	EventToolCall
)

func (k StreamEventKind) String() string {
	switch k {
	case EventThoughtDelta:
		return "ThoughtDelta"
	case EventTextDelta:
		return "TextDelta"
	case EventToolCall:
		return "ToolCall"
	default:
		return "Unknown"
	}
}

// StreamEvent represents a discrete, strongly-typed event emitted by StreamLexer.
type StreamEvent struct {
	Kind     StreamEventKind
	Text     string
	ToolCall *domain.ToolCall
}

// StreamLexer defines a stateful lexer that processes streaming chunks and emits StreamEvents.
type StreamLexer interface {
	// Feed receives a raw streaming token or chunk and returns zero or more parsed events.
	Feed(chunk string) []StreamEvent

	// Close flushes any buffered content at end of stream (EOS / done: true)
	// and applies end-of-stream fallbacks (e.g. unclosed-thought tool promotion).
	Close() ([]StreamEvent, error)
}

// WireCodec defines the contract for serializing context into raw model prompts and lexing raw output streams.
type WireCodec interface {
	// EncodePrompt renders system prompt, tool declarations, and canonical turn history into a deterministic raw prompt string.
	// CRITICAL: Must sort all map keys alphabetically so prefix bytes remain 100% identical across turns for KV caching.
	EncodePrompt(sys string, tools []ToolDefinition, history []TurnNode) (string, error)

	// NewStreamLexer creates a stateful byte/chunk lexer that emits canonical stream events (ThoughtDelta, TextDelta, ToolCall).
	NewStreamLexer() StreamLexer

	// StopSequences returns stop sequences to pass to Ollama options.stop.
	StopSequences() []string
}
