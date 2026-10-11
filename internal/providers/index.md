---
type: Concept
title: "Package internal/providers Index"
description: "LLM provider drivers (Ollama, OpenAI, Remote Daemon SSE, Mock), wire serialization protocols, and streaming parsers (ADR-006, ADR-018)."
tags:
  - please
  - providers
  - llm
  - index
timestamp: "2026-10-11T10:23:00-07:00"
---

# Package internal/providers Index (`internal/providers`)

The `internal/providers` package (Layer 1: Infrastructure) encapsulates all LLM API drivers, wire protocols, and streaming parsers behind the unified `LLMProvider` interface, per [ADR-006](../../decisions/006-modular-providers-extraction.md) and [ADR-018](../../decisions/018-package-stratification-and-domain-decoupling.md).

> [!NOTE]
> **Decoupled Schema Boundary**: Per [ADR-018](../../decisions/018-package-stratification-and-domain-decoupling.md), `internal/providers` imports only `internal/domain` (Layer 0) and never imports `internal/tools`. Tool schemas are passed into providers via `[]domain.ToolDefinition` and model parameters via `domain.ModelOptions`.

## Core Files & Components

* [provider.go](provider.go) - Defines the `LLMProvider` interface (`GenerateResponse`, `GenerateResponseStream`), endpoint URL normalizers (`NormalizeOllamaEndpoint`, `NormalizeOpenAIEndpoint`), and approval channel hooks.
* [ollama.go](ollama.go) - Local Ollama driver consuming `/api/chat` with native tool definitions, multimodal image payloads, reasoning (`thinking`) stream demuxing, and intra-turn working memory continuity.
* [openai.go](openai.go) - OpenAI and OpenAI-compatible (`/v1/chat/completions`) driver supporting streaming tool calls and reasoning token deltas (`reasoning_content` / `reasoning` for DeepSeek-R1, Gemma 4, MLX, and OpenRouter).
* [remote.go](remote.go) - `RemoteDaemonProvider` consuming the `POST /api/v1/chat/stream` SSE wire protocol to drive `please connect` clients with session ID propagation (`X-Please-Session-ID`).
* [mock.go](mock.go) - Deterministic `MockLLMProvider` for fast hermetic unit tests and Living Scenarios ([ADR-015](../../decisions/015-living-scenarios-and-test-subsystem-decomposition.md)).
* [provider_test.go](provider_test.go) & [remote_test.go](remote_test.go) - Hermetic test suites verifying streaming parsers, endpoint normalization, reasoning token demuxing, and SSE reconnections.
