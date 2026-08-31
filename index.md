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

`Please` is a lightweight conversation harness designed for dynamic interaction with Large Language Models (LLMs). Originating as a terminal utility 30 years ago, it models conversation histories as a Directed Acyclic Graph (DAG) of message nodes, supporting context resonance calculations, and native streaming playback. It is evolving into a multi-platform Swift Package targeting macOS and iPadOS alongside its Go CLI baseline.

## Subdirectories & Clients

*   [please-swift](../please-swift/index.md) - Native macOS / iPadOS client application (Subway Map DAG visualizer, 3-tier LOD, express spine, Supernodes).
*   [decisions](decisions/index.md) - Architecture Decision Records (ADRs) tracking design debates and key trade-offs.
*   [internal/engine](internal/engine/index.md) - Core engine package managing graph structures, SQLite storage, LLM providers, and tools.
*   [internal/server](internal/server/index.md) - Engine daemon, REST v1 API, SSE streaming protocol, and 20-year internal PKI cert generator.
*   [internal/tui](internal/tui/index.md) - Bubble Tea TUI state machine and handle rendering.
*   [please-cli](cmd/please/index.md) - The primary CLI entry point, providing various operational modes including TUI, Daemon, and Remote Client.

## Concepts & Architecture

*   [bootstrap_memory](GEMINI.md) - The bootstrap intent document detailing architecture overview and development conventions.
*   [daemon_protocol](docs/daemon_protocol_spec.md) - Authoritative REST v1 and SSE streaming wire protocol specification for multi-platform clients.
*   [swift_apple_evolution](decisions/004-swift-apple-ecosystem-evolution.md) - ADR 004: Swift port strategy, iPad Mini device targeting, and Intel Mac dev bridge strategy.
*   [context_resonance](context_resonance.md) - Dynamic Context Resonance Scoring algorithm (`V = (W * C) * e^(-k * Δt)`) to prune old tool outputs.
*   [natural_pacing](natural_pacing.md) - Natural reading-pace stream buffering with punctuation-sensitive pauses.
