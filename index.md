---
type: Concept
title: "Please Project Knowledge Index"
description: "Central knowledge index for Please: a DAG-based conversation harness, multi-session agent runtime, TUI, headless daemon, and ACP server."
tags:
  - please
  - go
  - index
timestamp: "2026-10-11T10:21:00-07:00"
---

# Please Project Knowledge Index

`Please` is a lightweight, high-performance conversation harness and multi-session agent runtime designed for dynamic interaction with Large Language Models (LLMs). It models conversation histories as a Directed Acyclic Graph (DAG) of message nodes with branch-isolated compactions, cache-stable context shaping (`ContextShaper`), three-tier memory (`SmartReceipt` out-of-band telemetry paging + FTS5 cybernetic recall), and native streaming playback.

With `v0.3.1`, `Please` unifies all execution surfaces—standalone Bubble Tea TUI (`please`), headless multi-session daemon (`please serve`), remote TUI client (`please connect`), and IDE Agent Client Protocol (`please acp`)—onto the canonical `SessionHarness` execution pipeline, complete with native delegated subagent topologies (`spawn_subagent`) and out-of-tree Git worktree sandboxing.

## Subdirectories & Packages (Four-Tier Architecture)

Per [ADR-018](decisions/018-package-stratification-and-domain-decoupling.md), packages are organized into a strict four-tier downward dependency hierarchy:

### Layer 3: Presentation, Daemon & Entrypoints
* [cmd/please](cmd/please/index.md) - Primary CLI entry point (`please`, `please init`, `please serve`, `please connect`, `please acp`, `please inspect`, `please context`, `please memory`, `please vault`, `please cert`).
* [internal/tui](internal/tui/index.md) - Charm Bubble Tea TUI built on a hierarchical `ViewStack` ([ADR-017](decisions/017-tui-viewstack-and-hierarchical-focus-architecture.md)), Lipgloss rendering, DAG `/map` visualizer, `/memories` deck, phonic bell staging ([ADR-013](decisions/013-acoustic-theatrics-phonic-staging-and-talon-tap-telemetry.md)), and natural token pacing.
* [internal/server](internal/server/index.md) - Headless daemon (`please serve`), REST v1 API, SSE streaming protocol, per-session `SessionActor` mailboxes, and 20-year internal PKI cert generator.
* [internal/acp](internal/acp/index.md) - Agent Client Protocol (ACP) stdio JSON-RPC server (`please acp` / `please-acp`), interactive tool permission gates, and Xcode quarantine shim ([ADR-012](decisions/012-agent-client-protocol-support.md)).

### Layer 2: Core Orchestration & Lifecycle
* [internal/engine](internal/engine/index.md) - Canonical `SessionHarness` ([ADR-019](decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md)), `LocalHarnessProvider`, pluggable `ContextShaper` projections ([ADR-020](decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md)), `SmartReceipt` synthesis ([ADR-021](decisions/021-tiered-sensory-storage-and-observation-compaction.md)), subagent delegation coordinator ([ADR-022](decisions/022-native-delegated-multi-agent-topologies.md)), and ambient peripheral telemetry.

### Layer 1: Infrastructure, Capabilities & Subsystems
* [internal/graph](internal/graph/index.md) - Pure in-memory DAG conversation `Graph` and `Node` structures with cycle detection ([ADR-010](decisions/010-pure-dag-graph-extraction.md)).
* [internal/storage](internal/storage/index.md) - Persistence abstractions: SQLite (WAL mode) for DAG `nodes`, `sessions`, `observation_blobs`, and FTS5 `memories`, plus JSONL, remote HTTP proxy, and AES-256-GCM vault encryption ([ADR-009](decisions/009-modular-storage-extraction.md), [ADR-014](decisions/014-persistent-agent-memory-and-cybernetic-recall.md), [ADR-021](decisions/021-tiered-sensory-storage-and-observation-compaction.md)).
* [internal/tools](internal/tools/index.md) - Tool registry, 3-tier sandbox policy enforcement (`strict`, `standard`, `permissive`), sensory/mutation/execution tools, `inspect_receipt` paging, cybernetic `memory_*` tools, and `spawn_subagent` delegation ([ADR-005](decisions/005-modular-tools-extraction.md), [ADR-011](decisions/011-agent-sandboxing-execution-isolation.md)).
* [internal/providers](internal/providers/index.md) - LLM provider drivers (Ollama, OpenAI, Remote Daemon SSE, Mock), wire protocols, and reasoning stream parsers ([ADR-006](decisions/006-modular-providers-extraction.md)).
* [internal/config](internal/config/index.md) - Configuration loading, `.please/` workspace discovery ladder ([ADR-016](decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md)), and Schema v2 migration utilities ([ADR-008](decisions/008-modular-config-extraction.md)).
* [internal/worktree](internal/worktree/index.md) - Ephemeral out-of-tree Git worktree manager for sandboxed concurrent sessions and delegated subagents ([ADR-007](decisions/007-multi-session-daemon-branch-concurrency.md), [ADR-022](decisions/022-native-delegated-multi-agent-topologies.md)).

### Layer 0: Core Domain Primitives
* [internal/domain](internal/domain/index.md) - Zero-dependency shared domain models (`Message`, `Role`, `ToolCall`, `ToolObservation`, `SmartReceipt`, `ToolDefinition`, `ModelOptions`, TLS cert helpers) ([ADR-018](decisions/018-package-stratification-and-domain-decoupling.md)).

## Architecture Decisions & Specifications

* [decisions](decisions/index.md) - Complete index of Architecture Decision Records (`ADR-001` through `ADR-022`).
* [bootstrap_memory](GEMINI.md) - Agent wakeup orientation document detailing architectural layers, memory tiers, and development conventions.
* [v0.3.0_roadmap](docs/roadmap_v0.3.0.md) - Authoritative v0.3.0 architectural blueprint for lifecycle convergence, context shaping, three-tier memory, and delegated agents.
* [daemon_protocol](docs/daemon_protocol_spec.md) - Authoritative REST v1 and SSE streaming wire protocol specification for multi-platform clients.
* [context_resonance](docs/context_resonance.md) - Cache-stable `ContextShaper` mathematical projections ([ADR-020](decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md)) and `SmartReceipt` out-of-band telemetry paging ([ADR-021](decisions/021-tiered-sensory-storage-and-observation-compaction.md)).
* [natural_pacing](docs/natural_pacing.md) - Natural reading-pace stream buffering with punctuation-sensitive pauses.

## External & Companion Resources

* [please-swift](../please-swift/index.md) - Native macOS / iPadOS client application specification (Subway Map DAG visualizer, 3-tier LOD, express spine, Supernodes; [ADR-004](decisions/004-swift-apple-ecosystem-evolution.md)).
* [examples/configs](examples/configs/README.md) - Ready-to-use Schema v2 configuration presets for local Ollama, Apple Silicon MLX, OpenRouter, and headless daemon deployments.
* [story](story/index.md) & [Lore](Lore/GEMINI.md) - Narrative worldbuilding, George (Lore-Warden of the Scriptorium), and living chronicles of the architecture.
