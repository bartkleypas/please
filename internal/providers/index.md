---
type: Concept
title: "Package internal/providers Index"
description: "LLM provider drivers, wire protocols, streaming parsers, and remote daemon client (ADR-006)."
tags:
  - please
  - providers
  - llm
  - index
timestamp: "2026-09-10T11:42:00-07:00"
---

# Package internal/providers Index

The `internal/providers` package encapsulates all LLM API drivers, wire protocols, and streaming parsers behind the unified `Provider` interface, per [ADR-006](../../decisions/006-modular-providers-extraction.md).

## Core Files & Components

*   [provider.go](provider.go) - Defines the `Provider` interface, token chunk structs, and streaming callback types.
*   [options.go](options.go) - Common inference configuration parameters (temperature, max tokens, top-p, stop sequences).
*   [ollama.go](ollama.go) - Local Ollama driver consuming the Ollama HTTP streaming API with system prompts and native function calling.
*   [openai.go](openai.go) - OpenAI and OpenAI-compatible driver supporting reasoning/thinking tokens (DeepSeek, Gemma, Claude/GPT via proxies).
*   [remote.go](remote.go) - `RemoteDaemonProvider` consuming the SSE `/api/v1/chat/stream` protocol to drive `please connect`.
*   [mock.go](mock.go) - Deterministic mock provider for fast offline unit tests and simulation of streaming tokens/tool calls.
*   [provider_test.go](provider_test.go) & [remote_test.go](remote_test.go) - Test suites covering streaming parsers, reconnections, and auth failures.
