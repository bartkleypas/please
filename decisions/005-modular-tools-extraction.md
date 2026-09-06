---
type: Decision
title: "005: Modular Tools Subsystem & Sandbox Extraction"
description: "Architecture decision record proposing the extraction and decomposition of tool execution, sandboxing, and default tool implementations from internal/engine into a dedicated internal/tools package."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - tools
  - refactor
timestamp: "2026-09-05T16:15:00-07:00"
---

# ADR 005: Modular Tools Subsystem & Sandbox Extraction

## Status

Accepted

## Context

The `internal/engine` package has become the central gravitational well of `please`, encompassing over 30 Go source files (~200 KB) responsible for:
1. DAG conversation graph models and in-memory branch navigation (`graph.go`, `node.go`).
2. SQLite relational persistence and WAL-mode synchronization (`storage.go`).
3. LLM context construction, resonance pruning, and telemetry envelopes (`service.go`, `telemetry.go`).
4. LLM provider drivers and streaming SSE multiplexing (`llm.go`, `openai.go`, `remote.go`).
5. Host tool registry, command sandboxing, and concrete file/command execution handlers (`tool.go`, `tool_defaults.go`, `sandbox.go`).

### The Agentic Pagination Tension

Modern autonomous coding agents (such as George operating via `read_file`) maintain strict pagination limits (typically 150 lines / ~8 KB per view turn) to avoid context clobbering and attentional degradation. 

Within `internal/engine`, `tool_defaults.go` has swollen to **765 lines (~22 KB)**. Inspecting or modifying this single file requires 5–6 pagination continuation turns. This "tall code tree" induces agentic disorientation, increases trajectory pressure, and slows down iterative maintenance.

### Why Tools First (and Not Provider)?

Extracting the LLM provider drivers (`llm.go`, `openai.go`, `remote.go`) into an `internal/provider` package is an eventual architectural desire. However, `provider` currently exhibits high-risk bidirectional coupling with core engine abstractions:
* Providers consume and produce `Message`, `Node`, `ThoughtProcess`, `ToolCall`, and `ModelOptions`.
* Streaming loops tightly integrate with `service.go` and server SSE events.
* Extracting `provider` today without an intermediate interface layer risks severe package boundary tangles and cyclic imports (`engine` $\leftrightarrow$ `provider`).

In contrast, the **tools subsystem is a natural leaf dependency**:
* Tools do not depend on `Graph`, `Node`, `Storage`, or LLM prompt formatting.
* A tool is simply an invokable contract: a name, description, JSON schema parameters, a `ToolCategory`, and an execution handler accepting a JSON argument map and returning a string result or error.
* In the current codebase, `tool_defaults.go` has only a single reference to `engine.Manager` (`func (m *Manager) RegisterDefaultTools(...)`).

Extracting the tools subsystem first offers maximum architectural relief with near-zero circular dependency risk.

---

## Decision

We will extract the tool registry, security sandbox, and default tool implementations from `internal/engine` into a dedicated package: **`internal/tools`**.

Furthermore, we will decompose the 765-line `tool_defaults.go` monolith into domain-focused, bite-sized files structured to stay well under 200 lines each:

```
internal/tools/
├── registry.go        # Tool, ToolCategory, ToolParam, ToolRegistry, deterministic sorting & policy filtering
├── sandbox.go         # SandboxPolicy, ValidateSafePath, shell command validation, pipeline operator safety
├── fs.go              # Filesystem tools: read_file, write_file, append_file, edit_file
├── search.go          # Discovery tools: list_directory, list_files_recursive, grep_search
├── exec.go            # Command runner: execute_command (host execution, timeouts, stdout/stderr capture)
├── defaults.go        # RegisterDefaultTools(r *ToolRegistry, workspaceDir string)
├── registry_test.go   # Registry tests (sorting, policy filtering, JSON parameter marshaling)
├── sandbox_test.go    # Sandbox security tests (path traversal, symlinks, pipeline safety)
└── tools_test.go      # End-to-end tool execution tests
```

### 1. File Responsibilities & Line Budgets

| File | Target Lines | Responsibilities |
| :--- | :--- | :--- |
| `registry.go` | ~120 lines | Definition of `Tool`, `ToolCategory` (`Sensory`, `Mutate`, `Execute`), `ToolParam`, `ToolRegistry`, deterministic family sorting (`GetTools`), and policy-aware filtering (`GetToolsForPolicy`). |
| `sandbox.go` | ~130 lines | `SandboxPolicy`, `ValidateSafePath` (resolving symlinks, protecting parent directory escapes), command whitelist enforcement, shell metacharacter blocking. |
| `fs.go` | ~180 lines | Implementation of `read_file` (with line slicing & pagination guards), `write_file` (with atomic write & overwrite flags), `append_file` (boundary normalization), and `edit_file` (exact string replacement). |
| `search.go` | ~130 lines | Implementation of `list_directory`, `list_files_recursive` (depth-limited, ignoring `.git`), and `grep_search` (bounded regex matching). |
| `exec.go` | ~90 lines | Implementation of `execute_command` with context cancellation, timeout enforcement, output trimming, and exit code capture. |
| `defaults.go` | ~60 lines | Factory function `RegisterDefaultTools(registry *ToolRegistry, workspaceDir string)` wiring all standard tools into a registry instance. |

### 2. Engine Integration Boundary

`internal/engine.Manager` will retain a reference to the tool registry via composition:

```go
package engine

import "please/internal/tools"

type Manager struct {
    // ...
    Tools *tools.ToolRegistry
    // ...
}
```

Calls previously dispatched through `m.Tools` in `service.go`, `stream.go`, and `server.go` will invoke the new `*tools.ToolRegistry` methods directly.

---

## Alternatives Considered

1. **In-place file splitting within `internal/engine`**:
   * *Alternative*: Split `tool_defaults.go` into `engine/tool_fs.go`, `engine/tool_search.go`, and `engine/tool_exec.go` without creating a new package.
   * *Drawback*: Leaves `internal/engine` bloated with ~35+ files and keeps execution/sandbox runtime concerns intermingled with cognitive DAG operations.
2. **Simultaneous extraction of `provider` and `tools`**:
   * *Alternative*: Extract both `internal/provider` and `internal/tools` in a single large refactoring pass.
   * *Drawback*: High blast radius. Unwinding `provider` package boundaries requires refactoring `Node`, `Message`, and streaming types, compounding risk and complicating test verification.

---

## Consequences

### Positive
* **Agent Readability**: Every tool file in `internal/tools/` fits comfortably within a single agentic `read_file` turn (<200 lines), eliminating multi-turn pagination fatigue.
* **Zero Circular Dependencies**: `internal/tools` imports standard Go library packages (`os`, `path/filepath`, `exec`, etc.) and zero packages from `internal/engine`.
* **Isolated Security Testing**: Path sandboxing, pipeline operator screening, and command restrictions can be rigorously unit-tested in isolation without instantiating SQLite databases or DAG graph managers.
* **Architectural Blueprint**: Successfully decouples runtime execution from the cognitive engine, establishing a clean, tested precedent before tackling the higher-risk `provider` extraction.

### Negative
* **Import Path Updates**: Requires updating imports from `please/internal/engine` to `please/internal/tools` across `service.go`, `service_test.go`, `stream.go`, `server.go`, and CLI runners.

---

## References

* ADR 002: SQLite WAL-Mode Storage Engine for DAG Persistence
* ADR 003: Ephemeral Leaf Telemetry & Pure Root Persona Architecture
* ADR 004: Swift Port Strategy & iPad Mini Device Jump
