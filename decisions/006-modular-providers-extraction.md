---
type: Decision
title: "ADR 006: Modular LLM Providers Subsystem Extraction"
description: "Extraction of LLM model provider drivers (Ollama, OpenAI, Remote Daemon, Mock), wire protocols, and message contracts from internal/engine into a dedicated internal/providers package."
tags:
  - please
  - architecture
  - adr
  - providers
  - decoupling
timestamp: "2026-09-07T07:50:00-07:00"
---

# ADR 006: Modular LLM Providers Subsystem Extraction

## Status

Accepted

## Context

Following the successful extraction of the tools subsystem into `internal/tools` ([ADR 005](005-modular-tools-extraction.md)), `internal/engine` remained responsible for two conceptually distinct concerns:
1. **The Cognitive Core**: The conversation DAG graph model (`graph.go`, `node.go`), SQLite storage (`storage.go`), context resonance pruning, ephemeral leaf telemetry envelopes (`service.go`, `telemetry.go`), and signat steering (`signat.go`).
2. **The LLM Drivers**: Backend wire serialization, HTTP streaming readers, and reasoning/token demuxing for Ollama (`llm.go`), OpenAI-compatible backends (`openai.go`), remote Please engine daemons (`remote.go`), and test harnesses (`mock.go`).

In ADR 005, extracting providers was postponed because of perceived high coupling with core engine abstractions (`Message`, `Node`, `Storage`, and streaming events).

### Architectural Re-examination

Close inspection revealed that the coupling was far narrower and cleaner than initially assumed:
* **Providers never touch `Node` or `Graph`**: Providers strictly consume and emit `Message`, `tools.Tool`, `ToolCall`, and `ToolObservation`.
* **The Remote Daemon Ambiguity**: In `remote.go`, `RemoteDaemonStorage` (which implements `Storage` and touches `Node`/`Graph`) was co-located in the same source file as `RemoteDaemonProvider` (which implements `LLMProvider`). Decoupling `RemoteDaemonStorage` into `internal/engine/storage_remote.go` resolved the storage boundary cleanly.
* **Strict Acyclic Dependency Hierarchy**:
  ```
                 internal/tools (leaf)
                       ▲
                       │
                internal/providers
                       ▲
                       │
                internal/engine (Graph, SQLite, Node, Service)
                   ▲        ▲
                   │        │
            internal/server internal/tui
                   ▲        ▲
                   │        │
                   cmd/please
  ```
  `internal/providers` imports `internal/tools` and standard library packages, with **zero** imports of `internal/engine`.

---

## Decision

We have extracted all LLM provider drivers, wire serialization schemas, and core message contracts from `internal/engine` into a dedicated package: **`internal/providers`**.

### Package Structure

```
internal/providers/
├── provider.go       # Provider & LLMProvider interfaces, Role, ToolCall, ToolObservation, Message
├── options.go        # ModelOptions inference & sampling parameters
├── ollama.go         # OllamaProvider implementation & JSON wire schemas
├── openai.go         # OpenAIProvider implementation & reasoning demuxing
├── remote.go         # RemoteDaemonProvider & ResolveCACert
├── mock.go           # MockLLMProvider for deterministic unit testing
├── provider_test.go  # Unit tests for options mapping, wire serialization, and reasoning extraction
└── remote_test.go    # Unit tests for SSE stream demuxing and CA cert resolution
```

### Engine Integration via Go Type Aliases

To preserve 100% backward compatibility across `internal/server`, `internal/tui`, and external callers, `internal/engine` re-exports provider types:

```go
package engine

import "github.com/bartkleypas/please/internal/providers"

type LLMProvider = providers.Provider
type Provider = providers.Provider
type Message = providers.Message
type Role = providers.Role
type ToolCall = providers.ToolCall
type ToolObservation = providers.ToolObservation
type ModelOptions = providers.ModelOptions

type OllamaProvider = providers.OllamaProvider
var NewOllamaProvider = providers.NewOllamaProvider

type OpenAIProvider = providers.OpenAIProvider
var NewOpenAIProvider = providers.NewOpenAIProvider

type RemoteDaemonProvider = providers.RemoteDaemonProvider
var NewRemoteDaemonProvider = providers.NewRemoteDaemonProvider
var ResolveCACert = providers.ResolveCACert

type MockLLMProvider = providers.MockLLMProvider
```

`RemoteDaemonStorage` remains within `internal/engine` in [storage_remote.go](../internal/engine/storage_remote.go), where it cleanly implements `Storage` alongside `SQLiteStorage` and `JSONLStorage`.

---

## Consequences

### Positive
* **Decoupled Architecture**: LLM provider implementations (streaming loops, wire payloads, headers, reasoning parsers) are fully isolated from the conversation graph and database storage layers.
* **Zero Circular Dependencies**: The package graph forms a strict, acyclic DAG (`tools` $\rightarrow$ `providers` $\rightarrow$ `engine` $\rightarrow$ `server`/`tui`).
* **Dramatic Reduction in Engine Footprint**: Removed ~1,350 lines of provider code and test harnesses from `internal/engine`. `llm.go` shrank from 379 lines to a clean 24-line re-export facade.
* **Isolated Testing**: Provider options mapping, reasoning token extraction, and stream event parsers are tested independently without SQLite initialization or workspace disk dependencies.
* **Extensibility**: Adding new provider backends (e.g., Anthropic Claude native, Google Gemini, MLX Swift bridge) only touches `internal/providers`.

### Negative
* **Additional Package**: Introduces one more package import boundary in the repository.

---

## References

* [ADR 002: SQLite WAL-Mode Storage Engine for DAG Persistence](002-graph-sqlite-storage.md)
* [ADR 003: Ephemeral Leaf Telemetry & Pure Root Persona Architecture](003-ephemeral-leaf-telemetry-pure-root-persona.md)
* [ADR 005: Modular Tools Subsystem Extraction](005-modular-tools-extraction.md)
