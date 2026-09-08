---
type: Decision
title: "ADR 010: Pure In-Memory DAG Graph Subsystem Extraction"
description: "Extraction of the foundational conversation Node data contracts and chronological in-memory DAG Graph data structure from internal/engine into a dedicated internal/graph package."
tags:
  - please
  - architecture
  - adr
  - graph
  - dag
  - decoupling
timestamp: "2026-09-07T18:37:00-07:00"
---

# ADR 010: Pure In-Memory DAG Graph Subsystem Extraction

## Status

Proposed

## Context

The core abstraction distinguishing `Please` from conventional linear chat applications is its **Directed Acyclic Graph (DAG)** conversation model ([ADR 002](002-graph-sqlite-storage.md)). 

Currently, this core data structure is embedded directly inside `internal/engine`:
* **`node.go`** (46 lines): `Node` struct, role constants (`RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool`, `RoleSummary`), tool call associations, and side-channel observation envelopes.
* **`graph.go`** (210 lines): Thread-safe `Graph` struct (`sync.RWMutex`), timestamp-sorted child indexation (`Children map[string][]string`), root tracking (`Roots []string`), node lookup, and lineage path traversal (`GetPath(nodeID)`).
* **`graph_test.go`** (123 lines): Tests verifying timestamp re-sorting, DAG branching, and path recovery.

### The Foundational Leaf Opportunity
An inspection of `node.go` and `graph.go` demonstrates that the conversation DAG is **pure mathematical state**:
* It has **no I/O dependencies** (no disk access, no SQLite, no network sockets).
* It depends only on the standard library (`errors`, `fmt`, `sort`, `strings`, `sync`, `time`) and `internal/providers` (for `Role` and `ToolCall` contracts).
* Both the storage layer (`internal/storage`) and the orchestration layer (`internal/engine`) require `Node` and `Graph` definitions to operate.

Leaving `Graph` inside `internal/engine` creates an awkward architectural knot: persistence drivers cannot reference `Node` without importing `internal/engine`.

---

## Decision

We will extract the conversation `Node` contract and the in-memory `Graph` DAG engine from `internal/engine` into a dedicated leaf package: **`internal/graph`**.

### Target Package Structure

```
internal/graph/
├── node.go          # Node struct, Role constants, ToolCall attachments
├── graph.go         # Graph struct, thread-safe mutations, path traversal
└── graph_test.go    # Unit tests for DAG construction, sorting, and path traversal
```

### Dependency Architecture

With `internal/graph` extracted as a dedicated leaf, the full dependency tree of the Please codebase becomes strictly acyclic, layered, and modular:

```
                  internal/tools (leaf)
                         ▲
                         │
                  internal/providers
                   ▲            ▲
                   │            │
            internal/config  internal/graph (leaf DAG)
                   ▲            ▲
                   │            │
                   │     internal/storage (SQLite, WAL, Remote)
                   │            ▲
                   │            │
                   └─── internal/engine (Manager, Resonance, Pacing)
                        ▲          ▲
                        │          │
                 internal/server internal/tui
                        ▲          ▲
                        │          │
                        cmd/please
```

### Engine Backward Compatibility via Type Aliases

`internal/engine` will re-export all graph types to ensure zero breaking changes across existing callers:

```go
package engine

import "github.com/bartkleypas/please/internal/graph"

type Node = graph.Node
type Graph = graph.Graph

var NewGraph = graph.NewGraph
var ErrNodeNotFound = graph.ErrNodeNotFound
```

---

## Consequences

### Positive
* **Pure Mathematical Foundation**: The conversation DAG becomes a self-contained, completely testable in-memory data structure with zero side effects.
* **Unlocks Clean Storage Boundary**: `internal/storage` can import `internal/graph` directly without touching `internal/engine`.
* **Primes Engine for `v0.2.0`**: Leaves `internal/engine` focused purely on cognitive mechanics: Context Resonance decay, stream pacing, tool execution dispatch, and supernode compaction.

### Negative
* Requires updating import paths across tests that directly construct `Graph` instances.
