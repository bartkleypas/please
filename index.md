# Please Project Knowledge Index

`Please` is a lightweight, Go-based Terminal User Interface (TUI) application designed for dynamic interaction with Large Language Models (LLMs). It models conversation histories as a Directed Acyclic Graph (DAG) of message nodes, supporting context resonance calculations, and native streaming playback.

## Subdirectories

*   [decisions](decisions/index.md) - Architecture Decision Records (ADRs) tracking design debates and key trade-offs.
*   [internal/engine](internal/engine/index.md) - Core engine package managing graph structures, SQLite storage, LLM providers, and tools.
*   [internal/tui](internal/tui/index.md) - Bubble Tea TUI state machine and handle rendering.

## Concepts

*   [bootstrap_memory](GEMINI.md) - The bootstrap intent document detailing architecture overview and development conventions.
*   [context_resonance](context_resonance.md) - Dynamic Context Resonance Scoring algorithm (`V = (W * C) * e^(-k * Δt)`) to prune old tool outputs.
*   [natural_pacing](natural_pacing.md) - Natural reading-pace stream buffering with punctuation-sensitive pauses.
