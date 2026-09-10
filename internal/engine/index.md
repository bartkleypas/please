---
type: Concept
title: "Package internal/engine Index"
description: "The internal/engine package contains the core logic of the Please application. It coordinates conversational memory graphs, database storage, LLM A..."
tags:
  - please
  - go
  - index
timestamp: "2026-07-05T14:44:42-07:00"
---

# Package internal/engine Index

The `internal/engine` package contains the core orchestration harness of the `Please` application. It coordinates conversational memory graphs, multi-turn LLM generation loops, function calling/tool execution, and streaming handlers across both standalone TUI and daemon environments.

## Core Files & Components

### 1. Canonical Session Harness & Providers
*   [harness.go](harness.go) - Declares `SessionHarness`, the unified, canonical engine responsible for coordinating LLM calls, streaming tokens, parsing function calls, executing host tools, and persisting nodes.
*   [local_provider.go](local_provider.go) - Implements `LocalHarnessProvider`, connecting Bubble Tea TUI instances directly to an in-process `SessionHarness` to match remote daemon behavior bit-for-bit.
*   [service.go](service.go) - Contains `Manager` which coordinates historical compactions (`CompactRange`), branch pruning (`PruneBranch`), and adversarial validation.

### 2. Function Calling & Tool Extraction
*   [tool_extract.go](tool_extract.go) - Extracts and parses tool calls from raw LLM responses (JSON blocks, markdown code blocks, XML blocks) and pairs observations deterministically by `ToolCallID`.
*   [signat.go](signat.go) - Manages tool schemas, signatures, and LLM function calling prompts.

### 3. Telemetry & Versioning
*   [telemetry.go](telemetry.go) - Lightweight event recording and diagnostic metrics tracking.
*   [version.go](version.go) - Canonical runtime version information for Please.

### 4. Modular Extractions (Decoupled Packages)
The following subsystems have been extracted from `internal/engine` into dedicated high-cohesion packages per ADRs:
*   [internal/graph](../graph/index.md) - Pure in-memory DAG `Graph` and `Node` structures (ADR-010).
*   [internal/tools](../tools/index.md) - Host tools, filesystem isolation, path virtualization, and command sandboxing (ADR-005).
*   [internal/providers](../providers/index.md) - LLM provider drivers (Ollama, OpenAI, Remote Daemon) (ADR-006).
*   [internal/config](../config/index.md) - Configuration loading and schemas (ADR-008).
*   [internal/storage](../storage/index.md) - SQLite, JSONL, and encrypted vault storage drivers (ADR-009).
*   [internal/worktree](../worktree/index.md) - Git worktree isolation for concurrent agent sessions (ADR-007).

