---
type: Concept
title: "Package internal/providers/wire Index"
description: "WireCodec and StreamLexer abstractions, raw model prompt serialization, and token-level streaming lexers for Gemma 4 (ADR-018, v0.3.2)."
tags:
  - please
  - providers
  - wire
  - gemma4
  - streaming
timestamp: "2026-10-14T13:00:00-07:00"
---

# Package internal/providers/wire Index (`internal/providers/wire`)

The `internal/providers/wire` package sits at **Layer 1** of the four-tier architecture ([ADR-018](../../decisions/018-package-stratification-and-domain-decoupling.md)). It defines raw wire codec abstractions and stateful streaming lexers for local LLM engines like Ollama's `POST /api/generate` with `"raw": true`.

## Core Components

* [codec.go](codec.go) - Core interfaces and event types:
  * `WireCodec`: Interface for deterministic prompt encoding, stateful stream lexer creation, and stop sequences.
  * `StreamLexer`: Stateful sliding-window stream processor (`Feed(chunk string)`, `Close()`).
  * `StreamEvent`: Canonical stream events (`EventThoughtDelta`, `EventTextDelta`, `EventToolCall`).
* [gemma4.go](gemma4.go) - Gemma 4 Wire Grammar implementation:
  * `Gemma4Codec`: Pure, deterministic prompt serializer adhering to directional pipe symmetry (`<|tag>` / `<tag|>`) and string quoting (`<|"|>`).
  * `Gemma4StreamLexer`: Stateful sliding lookahead lexer (up to 24-byte tail protection) with explicit state stack (`stateOutside`, `stateInsideThought`, `stateInsideToolCall`) and thought-bleed protection (model rehearsal vs. unclosed-thought EOS promotion).
