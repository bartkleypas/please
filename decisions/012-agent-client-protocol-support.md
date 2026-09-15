---
type: Decision
title: "012: Agent Client Protocol (ACP) Support and Interactive Consent Gate"
description: "Architecture decision record establishing Agent Client Protocol (ACP) support, stdio JSON-RPC agent harness integration, and human-in-the-loop tool authorization for modern IDEs (Zed, JetBrains, Xcode)."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - acp
  - protocol
  - interop
  - ide
  - security
timestamp: "2026-09-14T18:35:00-07:00"
---

# ADR 012: Agent Client Protocol (ACP) Support and Interactive Consent Gate

## Status

Proposed (Draft)

---

## Context

As `please` matures from a terminal-native conversational DAG and headless daemon (`please serve`) into an extensible agent harness, developers increasingly want to interact with `please` directly from within modern code editors (such as Zed, JetBrains, and Apple's Xcode).

Historically, integrating an external AI coding harness into multiple IDEs required authoring and maintaining bespoke editor extensions for each ecosystem. 

The emerging **Agent Client Protocol (ACP)**—standardized across Zed Industries, JetBrains, Coder, and Apple Xcode (under macOS Goldengate)—standardizes the boundary between code editors ("Clients") and AI coding agents ("Agents"). Analogous to how the Language Server Protocol (LSP) standardized compiler and language toolchain integration, ACP establishes a bidirectional, JSON-RPC 2.0 protocol (typically executed over `stdio`) that decouples UI rendering, diff inspection, and permission dialogs from agent runtime logic.

### 1. Alignment with ADR 011 (Interactive Consent Gate)
In [ADR 011: Agent Sandboxing, Credential Quarantine, and Execution Isolation](011-agent-sandboxing-execution-isolation.md), we established capability tiers (`strict`, `standard`, and `permissive`) and decreed that raw shell execution (`exec`) or sensitive workspace mutations must never occur autonomously without explicit human-in-the-loop authorization.

ACP provides a first-class, protocol-level primitive for this exact requirement:
* **`session/request_permission`**: When an agent prepares to run a gated tool (such as shell execution or file overwrites), it issues a blocking JSON-RPC request to the client. The editor renders an interactive permission dialog, and the turn proceeds only upon human consent.

### 2. Architectural Readiness of `please`
An analysis of the `please` codebase demonstrates that the engine is already 75–80% aligned with ACP requirements:
* **Decoupled Turn Loop**: [`SessionHarness`](../internal/engine/harness.go) already isolates the turn lifecycle, tool execution loop, context assembly, and token/thought streaming from the presentation layer.
* **Serialized Session Actors**: [`SessionActor`](../internal/server/session_actor.go) and `SessionActorRegistry` already manage sequential turn queues and Git worktree isolation per session.
* **1-to-1 Event Telemetry**: `engine.HarnessEvent` (`token`, `thought`, `tool_call`, `tool_result`, `node_complete`, `error`) maps directly to ACP's `session/update` notifications (`SessionUpdateAgentMessageChunk`, `SessionUpdateAgentThoughtChunk`, `SessionUpdateToolCall`).
* **Ambient Editor Context**: [`engine.TurnRequest`](../internal/engine/harness.go) already accepts `ActiveFile`, `CursorLine`, and client context maps, which `SessionHarness` injects directly into `Manager.SetClientContext`.
* **State Persistence**: `please`'s SQLite WAL storage maps named session heads (`SaveSessionHead` / `GetSessionHead`) cleanly to ACP's linear session model without requiring DAG schema modifications.

---

## Decision

We will implement native **Agent Client Protocol (ACP)** support in `please`, enabling modern code editors to launch and orchestrate `please` as a trusted external agent over standard I/O.

### 1. CLI Entrypoint (`please acp`)
We will add a dedicated subcommand in `cmd/please/main.go`:
```bash
please acp [-c config.json] [-v vault.db] [-w workspace_dir]
```
- **Transport**: Standard input (`os.Stdin`) and standard output (`os.Stdout`) using JSON-RPC 2.0 framing via the official Go SDK: `github.com/coder/acp-go-sdk`.
- **Stdout Hygiene**: Standard output is reserved strictly for protocol JSON-RPC messages. All internal diagnostic logging, debug messages, and tool stderr streams must write exclusively to `os.Stderr` or external log sinks.

### 2. Core Protocol Adapter (`internal/acp`)
We will create a new package `internal/acp` implementing the `acp.Agent` interface:

```
┌────────────────────────────────────────────────────────┐
│           Editor Client (Zed / JetBrains / Xcode)      │
└──────────────────────────┬─────────────────────────────┘
                           │ JSON-RPC 2.0 over stdio
                           ▼
┌────────────────────────────────────────────────────────┐
│                   please acp (Agent)                   │
│                                                        │
│  ┌──────────────────────────────────────────────────┐  │
│  │ internal/acp.Agent Adapter                       │  │
│  │ (Initialize, NewSession, Prompt, Cancel, Close)  │  │
│  └───────────────────────┬──────────────────────────┘  │
│                          │                             │
│     ┌────────────────────┼───────────────────┐         │
│     ▼                    ▼                   ▼         │
│ ┌───────────────┐ ┌───────────────┐ ┌────────────────┐ │
│ │ SessionActor  │ │ SessionHarness│ │ Tool Registry  │ │
│ │ (Worktrees)   │ │ (Turn Loop)   │ │ & Sandboxing   │ │
│ └───────┬───────┘ └───────┬───────┘ └────────┬───────┘ │
│         │                 │                  │         │
│         └─────────────────┼──────────────────┘         │
│                           ▼                            │
│                 SQLite WAL DAG Storage                 │
└────────────────────────────────────────────────────────┘
```

#### Lifecycle Methods
1. **`Initialize`**:
   - Negotiates protocol version.
   - Advertises agent information (`name: "please"`, version from `internal/engine/version.go`).
   - Declares agent capabilities: `sessionCapabilities.close = true`, `sessionCapabilities.list = true`, `sessionCapabilities.resume = true`.
2. **`NewSession`**:
   - Resolves workspace directory from request parameters.
   - Creates or links to a session head in SQLite storage.
   - Instantiates or retrieves a `SessionActor`.
3. **`ListSessions` & `ResumeSession`**:
   - Queries historical session heads from `storage.Storage`.
   - Reconstructs linear context along DAG lineage from the session head node.
4. **`Cancel`**:
   - Triggers cancellation on the active turn's `context.Context`, halting LLM generation and any in-flight tool execution cleanly.
5. **`CloseSession`**:
   - Flushes state, cancels background workers, and tears down any ephemeral Git worktrees allocated for that session.

### 3. Prompt Execution & Streaming Bridge
When the client calls `session/prompt`:
1. Convert `acp.PromptRequest` content blocks and editor telemetry (`ActiveFile`, `CursorLine`) into `engine.TurnRequest`.
2. Submit turn to `SessionActor` / `SessionHarness.ExecuteTurn` with a dedicated `eventCh`.
3. Stream incremental progress to the client via `client.SessionUpdate`:
   - `HarnessEventToken` $\rightarrow$ `SessionUpdateAgentMessageChunk`
   - `HarnessEventThought` $\rightarrow$ `SessionUpdateAgentThoughtChunk`
   - `HarnessEventToolCall` $\rightarrow$ `SessionUpdateToolCall` (status: `in_progress`)
   - `HarnessEventToolResult` $\rightarrow$ `SessionUpdateToolCall` (status: `completed` or `failed`)
   - `HarnessEventNodeComplete` $\rightarrow$ Return final `acp.PromptResponse` with turn completion status (`EndTurn`).

### 4. Interactive Consent Gate (`client.RequestPermission`)
To satisfy ADR 011 requirements:
1. When `SessionHarness` encounters a tool flagged as requiring user consent (e.g. `exec` in `permissive` mode, or mutating operations on sensitive targets):
2. The harness pauses turn execution and calls:
   ```go
   resp, err := client.RequestPermission(ctx, acp.RequestPermissionRequest{
       SessionId: sessionID,
       ToolCall:  toolCallData,
       Options:   []acp.PermissionOption{...},
   })
   ```
3. If the user rejects the permission or the turn is canceled, the tool invocation returns a permission-denied observation to the model, maintaining conversation safety without crashing the harness.

---

## Difficulty & Effort Assessment

* **Overall Difficulty**: **Medium–Low**
* **Time Estimate**: **3 to 5 engineering days** (~8–12 story points)

### Work Stream Breakdown

| Stream | Tasks | Estimate |
| :--- | :--- | :--- |
| **Stream 1: Wire & Transport** | Add `cmd/please/main.go` routing, import `github.com/coder/acp-go-sdk`, implement stdio connection harness, enforce stdout log redirection. | 0.5 – 1 day |
| **Stream 2: Core Adapter** | Implement `acp.Agent` (`Initialize`, `NewSession`, `ListSessions`, `ResumeSession`, `Cancel`, `CloseSession`), integrate with SQLite session storage. | 1 day |
| **Stream 3: Prompt Streaming** | Map `PromptRequest` to `TurnRequest`, bridge `HarnessEvent` channel to `client.SessionUpdate`, verify token/thought pacing. | 1 day |
| **Stream 4: Permission Gate** | Wire `client.RequestPermission` into tool execution loop for gated tools under ADR 011 policy. | 1 – 1.5 days |
| **Stream 5: Testing & Editor QA** | In-memory stdio mock integration tests, end-to-end verification in Zed and Xcode Agent Panels. | 0.5 – 1 day |

---

## Consequences

### Positive
- **Universal Editor Compatibility**: `please` immediately gains native support in Zed, JetBrains, Xcode, and any upcoming ACP-compliant editor without maintaining proprietary IDE plugins.
- **First-Class Human-in-the-Loop Safety**: Fulfills the interactive consent gate mandated by ADR 011 via native editor UI dialogs.
- **Zero Duplication**: Directly reuses existing `SessionHarness`, `SessionActor`, and `ToolRegistry` components.
- **Native Editor Context**: Seamlessly receives active file, selection, and cursor positions from the editor.

### Negative / Tradeoffs
- **External Dependency**: Adds `github.com/coder/acp-go-sdk` to `go.mod`. The SDK dependency must be pinned to insulate against upstream protocol schema churn.
- **Stdio Discipline**: Any rogue standard library print or uncaptured tool output directed to `stdout` will corrupt the JSON-RPC stream; requires strict linting and telemetry isolation.

---

## Next Steps

1. Add `github.com/coder/acp-go-sdk` to `go.mod`.
2. Implement `internal/acp` containing the `Agent` implementation and session event adapter.
3. Wire `please acp` into `cmd/please/main.go`.
4. Implement permission callback hook in `internal/engine/harness.go` to invoke `client.RequestPermission`.
5. Add configuration instructions and documentation for Zed external agent registration.
