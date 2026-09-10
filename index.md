---
type: Concept
title: "Please Project Knowledge Index"
description: "Please is a lightweight, Go-based Terminal User Interface (TUI) application designed for dynamic interaction with Large Language Models (LLMs). It ..."
tags:
  - please
  - go
  - index
timestamp: "2026-07-05T14:26:05-07:00"
---

# Please Project Knowledge Index

`Please` is a lightweight, high-performance conversation harness and multi-session agent runtime designed for dynamic interaction with Large Language Models (LLMs). It models conversation histories as a Directed Acyclic Graph (DAG) of message nodes with branch-isolated compactions, context resonance calculations, and native streaming playback. With v0.2.0, `Please` features a multi-session headless daemon (`please serve`), canonical `SessionHarness` orchestration, per-session actor mailboxes, and out-of-tree Git worktree sandboxing for concurrent agent execution.

## Subdirectories & Packages

*   [decisions](decisions/index.md) - Architecture Decision Records (ADRs) tracking design debates and key trade-offs (ADR-001 through ADR-010).
*   [cmd/please](cmd/please/index.md) - Primary CLI entry point (`please`, `please serve`, `please connect`, `please inspect`, `please context`, `please cert`).
*   [internal/config](internal/config/index.md) - Configuration loading, JSON schema validation, directory discovery, and migration utilities.
*   [internal/engine](internal/engine/index.md) - Canonical `SessionHarness`, `LocalHarnessProvider`, multi-turn tool execution loop, and legacy compatibility layers.
*   [internal/graph](internal/graph/index.md) - Pure in-memory DAG conversation `Graph` and `Node` structures with cycle detection.
*   [internal/providers](internal/providers/index.md) - LLM provider drivers (Ollama, OpenAI, Remote Daemon, Mock), wire protocols, and streaming parsers.
*   [internal/server](internal/server/index.md) - Headless daemon (`please serve`), REST v1 API, SSE streaming protocol, per-session `SessionActor` mailboxes, and 20-year internal PKI cert generator.
*   [internal/storage](internal/storage/index.md) - Persistence abstractions: SQLite (WAL mode), JSONL, remote proxy, and AES-256 vault encryption.
*   [internal/tools](internal/tools/index.md) - Tool registry, execution sandboxing, filesystem operations (with pagination & path virtualization), and search tools.
*   [internal/tui](internal/tui/index.md) - Charm Bubble Tea TUI state machine, Lipgloss rendering, DAG visualizer, and streaming token pacing.
*   [internal/worktree](internal/worktree/index.md) - Ephemeral out-of-tree Git worktree manager for sandboxed concurrent agent sessions.
*   [please-swift](../please-swift/index.md) - Native macOS / iPadOS client application specification (Subway Map DAG visualizer, 3-tier LOD, express spine, Supernodes).

## Concepts & Architecture

*   [bootstrap_memory](GEMINI.md) - The bootstrap intent document detailing architectural overview and development conventions.
*   [daemon_protocol](docs/daemon_protocol_spec.md) - Authoritative REST v1 and SSE streaming wire protocol specification for multi-platform clients.
*   [multi_session_concurrency](decisions/007-multi-session-daemon-branch-concurrency.md) - ADR-007: Multi-session daemon architecture, cursor decoupling, branch-isolated compactions, and Git worktree sandboxing.
*   [swift_apple_evolution](decisions/004-swift-apple-ecosystem-evolution.md) - ADR-004: Swift port strategy, iPad Mini device targeting, and Intel Mac dev bridge strategy.
*   [context_resonance](docs/context_resonance.md) - Dynamic Context Resonance Scoring algorithm (`V = (W * C) * e^(-k * Δt)`) to prune old tool outputs.
*   [natural_pacing](docs/natural_pacing.md) - Natural reading-pace stream buffering with punctuation-sensitive pauses.
