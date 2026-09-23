---
type: Decision
title: "018: Package Stratification, Domain Type Decoupling, and Engine Façade Deconstruction"
description: "Architecture decision record establishing strict four-tier package layering, extracting a zero-dependency domain layer (internal/domain), eliminating inverted leaf dependencies, and deconstructing the legacy internal/engine re-export facade."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - refactoring
  - dependencies
  - domain-driven-design
timestamp: "2026-09-23T08:15:00-07:00"
---

# ADR 018: Package Stratification, Domain Type Decoupling, and Engine Façade Deconstruction

## Status

Accepted / Implemented

---

## Context

Over the course of the project's development, `please` has progressively modularized its initial monolithic `internal/engine` package into focused subsystems:
* **ADR 005**: Extracted sensory, mutation, and execution tool implementations into `internal/tools`.
* **ADR 006**: Extracted LLM provider implementations (Ollama, OpenAI, Remote) into `internal/providers`.
* **ADR 008**: Extracted schema definitions, configuration loading, and path resolution into `internal/config`.
* **ADR 009**: Extracted SQLite WAL, JSONL, and remote storage backends into `internal/storage`.
* **ADR 010**: Extracted the conversation Node graph and DAG algorithms into `internal/graph`.

While each individual extraction successfully isolated concrete logic, the project avoided breaking changes at call sites by retaining backward-compatibility type aliases and forwarders inside `internal/engine`. Furthermore, foundational domain types were inadvertently placed into downstream leaf packages during extraction.

Recent visual analysis of the codebase's package dependency graph (`internal_clean.png` and `deps.png`) revealed a dense, tangled web of cross-package coupling—an architectural "fuzzy AST"—characterized by circular abstractions, inverted dependencies, and excessive coupling targets.

```
=== Most Imported Packages (Coupling Targets) ===
   5 internal/providers
   5 internal/engine
   4 internal/tools
   4 internal/storage
   4 internal/graph
   3 internal/worktree
   3 internal/config
   2 internal/server
   1 internal/tui
   1 internal/acp

=== Packages With Most Internal Dependencies ===
   7 cmd/please
   6 internal/engine
   5 test/scenarios/seeder
   4 internal/tui
   2 internal/storage
   2 internal/server
   2 internal/config
   2 internal/acp
   1 internal/providers
   1 internal/graph
```

### Pathological Coupling Patterns

Tracing the concrete import edges reveals four core architectural flaws:

#### 1. Domain Types Trapped in Leaf Packages (Inverted Dependencies)
Fundamental domain concepts that belong at the bedrock of the application were defined inside leaf implementation packages:
* **`internal/graph` imports `internal/providers`**: The core conversation DAG (`internal/graph/node.go`) imports `internal/providers` solely to re-alias `Role`, `ToolCall`, and `ToolObservation`. A pure graph data structure should never depend on third-party LLM provider drivers.
* **`internal/storage` imports `internal/providers`**: Persistence engines (`SQLiteStorage`, `JSONLStorage`) import `internal/providers` to persist `ToolObservation` and call `ResolveCACert`.
* **`internal/config` imports `internal/providers` and `internal/tools`**: Configuration parsing imports `internal/providers` for `ModelOptions` and `internal/tools` for `SandboxPolicyStandard`/`Strict`.

#### 2. Sideways Execution Coupling (`providers` -> `tools`)
`internal/providers` imports `internal/tools` solely to type-check `availableTools []tools.Tool` in `GenerateResponse` and `GenerateResponseStream`. The LLM provider driver only needs the schema metadata (name, description, parameter schema) to serialize tool specifications to JSON for Ollama or OpenAI. Binding providers directly to `tools.Tool` couples LLM communication to concrete host execution machinery (OS process spawning, file mutation, and memory databases).

#### 3. `internal/engine` as a Monolithic "God Façade"
`internal/engine` currently maintains 8+ forwarding files (`config.go`, `crypto.go`, `graph.go`, `llm.go`, `node.go`, `sandbox.go`, `storage.go`, `tool.go`) that do nothing except alias types from `config`, `graph`, `providers`, `tools`, and `storage`.
Because `internal/engine` re-exports everything, downstream packages (`cmd/please`, `internal/tui`, `internal/acp`, `internal/server`) import both `internal/engine` and the underlying packages unpredictably, defeating the purpose of modularization.

#### 4. Presentation Surfaces Bypassing Engine Boundaries
`internal/tui` imports `internal/engine`, `internal/server`, `internal/storage`, and `internal/worktree` directly, rather than interacting with a unified client or engine service abstraction.

---

## Decision

We establish a strict, four-tier acyclic package hierarchy and introduce a zero-dependency domain package to eliminate inverted dependencies.

### 1. Four-Tier Architectural Stratification

```
+-----------------------------------------------------------------+
| Layer 3: Presentation & User Interfaces                         |
|   cmd/please, internal/tui, internal/acp, internal/server       |
+-----------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------+
| Layer 2: Core Orchestration & Lifecycle                         |
|   internal/engine (Turn loop, SessionHarness, Tool Coordinator) |
+-----------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------+
| Layer 1: Infrastructure, Capabilities & Subsystems              |
|   internal/graph, storage, tools, providers, config, worktree   |
+-----------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------+
| Layer 0: Core Domain & Primitives (Zero Internal Dependencies)  |
|   internal/domain                                               |
+-----------------------------------------------------------------+
```

#### Layer Boundary Invariants:
1. **Downwards Only**: A package in Layer $N$ may only import packages in Layer $< N$. Sideways imports between peer packages in Layer 1 must be strictly justified or eliminated through Layer 0 domain abstractions.
2. **Layer 0 Hermeticity**: `internal/domain` has **zero** imports from `github.com/bartkleypas/please/...`. It depends only on the Go standard library. We can ensure this with CI tests, and should.
3. **No Direct Leaf Coupling in Providers**: `internal/providers` must not import `internal/tools`.
4. **No Provider Imports in Graph or Storage**: Neither `internal/graph` nor `internal/storage` may import `internal/providers`.

---

### 2. Creation of `internal/domain`

We introduce `internal/domain` to house canonical, cross-cutting primitives:

```go
package domain

// Roles in conversation nodes and LLM messages
type Role string
const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
    RoleSummary   Role = "summary"
)

// ToolCall represents an LLM's invocation request
type ToolCall struct {
    ID        string                 `json:"id"`
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
}

// ToolObservation represents the result or side-effect of executing a tool
type ToolObservation struct {
    CallID   string `json:"call_id"`
    ToolName string `json:"tool_name"`
    Output   string `json:"output"`
    Error    string `json:"error,omitempty"`
}

// Message represents a prompt, completion, or context frame
type Message struct {
    Role         Role              `json:"role"`
    Content      string            `json:"content"`
    Thought      string            `json:"thought,omitempty"`
    ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
    Observations []ToolObservation `json:"observations,omitempty"`
}

// ToolSpec represents the schema definition required by LLMs for function calling
type ToolSpec struct {
    Name        string      `json:"name"`
    Description string      `json:"description"`
    Parameters  interface{} `json:"parameters"`
    Interactive bool        `json:"interactive,omitempty"`
}

// ModelOptions represents sampling and runtime inference knobs
type ModelOptions struct {
    Temperature     float64 `json:"temperature"`
    TopP            float64 `json:"top_p"`
    MaxTokens       int     `json:"max_tokens"`
    FrequencyPenalty float64 `json:"frequency_penalty"`
    PresencePenalty  float64 `json:"presence_penalty"`
}

// SandboxPolicy represents security containment levels
type SandboxPolicy string
const (
    SandboxPolicyStandard SandboxPolicy = "standard"
    SandboxPolicyStrict   SandboxPolicy = "strict"
)
```

---

### 3. Decoupling Provider Function Calling from Concrete Tools

In `internal/providers`, the interface method signatures are updated to accept pure specifications rather than execution structs:

```diff
- func (p *OpenAIProvider) GenerateResponse(ctx context.Context, messages []Message, availableTools []tools.Tool) (*Message, error)
+ func (p *OpenAIProvider) GenerateResponse(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (*domain.Message, error)
```

`internal/tools.Tool` embeds or converts into `domain.ToolSpec`:
```go
func (t Tool) Spec() domain.ToolSpec {
    return domain.ToolSpec{
        Name:        t.Name,
        Description: t.Description,
        Parameters:  t.Parameters,
        Interactive: t.Interactive,
    }
}
```

The orchestration layer (`internal/engine`) bridges the two: it queries `ToolRegistry` for executable tools, transforms their specs for the provider, and handles the resulting `ToolCall` invocations against the registry.

---

### 4. Deconstructing the `internal/engine` Façade

We dismantle the re-export sprawl in `internal/engine`:
1. **Targeted Coordination**: `internal/engine` focuses purely on session lifecycle (`Manager`), the turn orchestration loop (`SessionHarness`), cognitive compaction, and tool dispatch.
2. **Alias Deprecation**: Callers in `cmd/please`, `internal/tui`, and `internal/acp` are migrated to import the canonical packages (`internal/domain`, `internal/graph`, `internal/storage`, `internal/config`) directly.
3. **Façade File Removal**: The pass-through alias files (`engine/node.go`, `engine/graph.go`, `engine/llm.go`, `engine/tool.go`, `engine/config.go`, `engine/storage.go`) are systematically removed.

---

## Visual Topology Comparison

### Before (Current Tangled Mesh)
```mermaid
graph TD
    cmd[cmd/please] --> tui[internal/tui]
    cmd --> acp[internal/acp]
    cmd --> server[internal/server]
    cmd --> engine[internal/engine]
    cmd --> storage[internal/storage]
    cmd --> config[internal/config]
    cmd --> graph[internal/graph]

    engine --> config
    engine --> graph
    engine --> providers[internal/providers]
    engine --> storage
    engine --> tools[internal/tools]
    engine --> worktree[internal/worktree]

    config -.-> providers
    config -.-> tools
    graph -.-> providers
    storage -.-> providers
    storage --> graph
    providers -.-> tools

    tui --> engine
    tui --> server
    tui --> storage
    tui --> worktree

    acp --> engine
    acp --> tools
    server --> engine
    server --> worktree
```

### After (Strict Four-Tier Hierarchy)
```mermaid
graph TD
    subgraph Layer 3: Presentation & Surfaces
        cmd[cmd/please]
        tui[internal/tui]
        acp[internal/acp]
        server[internal/server]
    end

    subgraph Layer 2: Orchestration
        engine[internal/engine]
    end

    subgraph Layer 1: Subsystems & Capabilities
        config[internal/config]
        graph[internal/graph]
        storage[internal/storage]
        tools[internal/tools]
        providers[internal/providers]
        worktree[internal/worktree]
    end

    subgraph Layer 0: Core Domain
        domain[internal/domain]
    end

    %% Presentation dependencies
    cmd --> tui
    cmd --> acp
    cmd --> server
    cmd --> engine
    cmd --> config
    tui --> engine
    tui --> domain
    acp --> engine
    acp --> domain
    server --> engine

    %% Orchestration dependencies
    engine --> config
    engine --> graph
    engine --> storage
    engine --> tools
    engine --> providers
    engine --> worktree
    engine --> domain

    %% Subsystems dependencies
    storage --> graph
    config --> domain
    graph --> domain
    storage --> domain
    tools --> domain
    providers --> domain

    %% Notice: ZERO sideways arrows between config, graph, storage, tools, and providers!
```

---

## Phased Implementation Plan

### Phase 0: Reminders
* Try not to insert new type aliases during the migration. We might *want* to, but resist the urge. "No parallel types traps".

### Phase 1: Foundational Domain Extraction (`internal/domain`)
* Create `internal/domain` with pure models (`Role`, `ToolCall`, `ToolObservation`, `Message`, `ToolSpec`, `ModelOptions`, `SandboxPolicy`).
* Add unit tests for `internal/domain`, and run them.
* Add testing routine for `internal/domain` package hermeticity. This is to help us protect the package boundries from our future self.
* Remove old struct declarations from `providers` and `tools`.
* Run a build (`go build ./...`), and let the compiler tell us where we need to re-attach our package imports to the new concrete floor.

### Phase 2: Decouple Layer 1 Subsystems
* **`internal/config`**: Update `config.go` to use `domain.ModelOptions` and `domain.SandboxPolicy`. Remove `internal/providers` and `internal/tools` imports.
* **`internal/storage`**: Update storage interfaces and SQLite/JSONL drivers to use `domain.ToolObservation` and `domain.ToolCall`. Move `ResolveCACert` into an appropriate network/crypto utility if needed. Remove `internal/providers` import.
* **`internal/graph`**: Update `node.go` to use `domain.Role`, `domain.ToolCall`, and `domain.ToolObservation`. Remove `internal/providers` import.

### Phase 3: Decouple Provider Function Calling
* Add `domain.ToolSpec` representation and `Tool.Spec()` helper in `internal/tools`.
* Update `internal/providers` interface (`GenerateResponse`, `GenerateResponseStream`) to accept `[]domain.ToolSpec`.
* Remove `internal/tools` import from `internal/providers`.

### Phase 4: Deconstruct `internal/engine` Façade
* Remove obsolete forwarding files in `internal/engine` (`node.go`, `graph.go`, `llm.go`, `tool.go`, `config.go`, `storage.go`).
* Confirm friction points in packages by running `go build ./...`.
* Run through the "punch list" of build faults and update the call sites to reference new canonical package locations. (No `engine.*` aliases)
* Verify with `go test ./...` and generate updated dependency graphs to validate clean layering.

---

## Consequences

### Positive
* **Acyclic, Intuitive Mental Model**: Eliminates the "fuzzy AST" and restores clear architectural sanity.
* **Hermetic Compilation Units**: Leaf subsystems (`config`, `graph`, `tools`, `providers`) compile in parallel with zero circular or sideways baggage.
* **Zero Host Tool Pollution in Providers**: LLM provider drivers deal only with wire contracts and JSON schemas, not OS execution mechanics.
* **Reduced Blast Radius**: Changes to tool implementations or provider network adapters no longer trigger re-compilations of storage, graph, or configuration packages.
* **Immediate Test Isolation**: Subsystem unit tests run significantly faster with fewer mock harnesses required.

### Negative
* **Import Path Migration**: Multiple internal packages will require updated import declarations and type references.
* **Temporary Transition Churn**: Code reviews during Phase 2-4 will see widespread diffs across `node.go`, `config.go`, and `service.go`.
