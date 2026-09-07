package engine

import (
	"github.com/bartkleypas/please/internal/providers"
)

// Re-export core provider abstractions from internal/providers for backward compatibility.
type LLMProvider = providers.Provider
type Provider = providers.Provider
type Message = providers.Message

type OllamaProvider = providers.OllamaProvider

var NewOllamaProvider = providers.NewOllamaProvider
var NormalizeOllamaEndpoint = providers.NormalizeOllamaEndpoint

type OpenAIProvider = providers.OpenAIProvider

var NewOpenAIProvider = providers.NewOpenAIProvider
var NormalizeOpenAIEndpoint = providers.NormalizeOpenAIEndpoint

type RemoteDaemonProvider = providers.RemoteDaemonProvider

var NewRemoteDaemonProvider = providers.NewRemoteDaemonProvider
var ResolveCACert = providers.ResolveCACert

type MockLLMProvider = providers.MockLLMProvider
