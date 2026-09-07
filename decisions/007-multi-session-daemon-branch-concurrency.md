---
type: Decision
title: "ADR 007: Multi-Session Daemon Architecture & Branch Concurrency"
description: "Analysis and architectural strategy for multi-client concurrency in please serve, addressing session isolation, head decoupling, branch-aware compaction, and workspace sandboxing."
tags:
  - please
  - architecture
  - adr
  - daemon
  - concurrency
  - multiplayer
  - branching
timestamp: "2026-09-07T11:00:00-07:00"
---

# ADR 007: Multi-Session Daemon Architecture & Branch Concurrency

## Status

Proposed

## Context

The `please serve` daemon and `please connect` client modes were originally designed as a **1:1 remote bridge**. The primary architectural goal was hardware bridging: offloading local inference, LLM provider drivers, and SQLite DAG persistence to a high-powered workstation or headless server, while allowing an operator to drive the terminal interface (`please connect`) from a thin laptop or mobile client over TLS and Server-Sent Events (SSE).

Because a 1:1 operator relationship was implicitly assumed, the daemon and client subsystems share a singular, monolithic session state:
* A single in-memory `Graph` with global root and child mappings ([internal/engine/graph.go](file:///Users/bart/Code/please/internal/engine/graph.go)).
* A single global SQLite storage backend ([internal/engine/storage.go](file:///Users/bart/Code/please/internal/engine/storage.go)).
* A shared physical working directory for tool execution (`exec`, `file_write`, `search`) via [internal/tools](file:///Users/bart/Code/please/internal/tools/).

### Problem Analysis: The Multi-Client Collision

When multiple operators or terminal windows invoke `please connect` against the same running `please serve` daemon, three critical failure modes emerge:

#### 1. TUI Cursor & Viewport Hijacking
In [internal/tui/events.go](file:///Users/bart/Code/please/internal/tui/events.go):
```go
case server.EventNodeSaved:
    if lastID != "" && (m.CurrentID == "" || m.TextInput.Value() == "") {
        m.CurrentID = lastID
    }
```
When any client saves a node (such as receiving a token chunk or submitting a prompt), the daemon broadcasts `EventNodeSaved` to all SSE subscribers. Any client currently idle or reviewing history with an empty input buffer has their active cursor (`m.CurrentID`) violently yanked forward to the newest node. Connected clients constantly fight over the camera.

#### 2. Compaction Temporal Rug-Pulls
In the DAG model, compaction condenses a linear trajectory of nodes into a synthetic `Supernode`, pruning or re-parenting intermediate nodes.
If **Client A** branches off Node 5 to explore an alternative implementation, while **Client B** triggers `/compact` across Nodes 1–8 on the main branch:
* The shared ancestors under Client A's branch are rewired or superseded.
* Client A receives an `EventBranchCompacted` event, finds their active `CurrentID` invalidated, and is forced to fallback to the root or the latest supernode.
* Client A experiences unexpected temporal erasure of their active working context.

#### 3. Workspace / Filesystem Collisions
Tools executed by the LLM (e.g. bash commands, file modifications, test suites) execute directly in the daemon's host filesystem environment.
If Client A and Client B are pursuing divergent hypotheses on separate branches:
* Client A's branch may write code changes to `main.go`.
* Client B's branch may simultaneously run `go test ./...`.
* The tool environment lacks branch-aware worktree isolation; changes made on one branch bleed instantly into the other branch's runtime.

#### 4. Provider & Generation Contention
The daemon manages a single LLM provider instance. Concurrent `/api/v1/chat/stream` requests from multiple clients submit interleaved requests to the underlying model provider without fair-share queuing, token priority, or context isolation.

---

## Strategic Alternatives

### Option A: Strict 1:1 Enforcement (Single-Operator Bridge)
Enforce that `please serve` is strictly a 1:1 remote bridge:
* The daemon accepts only one read-write active client session at a time.
* Additional incoming connections are either rejected or downgraded to a read-only **Observer Mode** (mirroring the primary operator's session without permitting prompts, tool execution, or compactions).

*Tradeoffs*: Very simple to implement, zero risk of data corruption or workspace collisions, but forecloses collaborative multi-agent or multi-operator workflows.

### Option B: Decoupled Multi-Session DAG Architecture (Full Collaboration)
Evolve the daemon into a multi-session host with branch isolation:
1. **Namespaced Session Heads**:
   - Replace the global `CurrentID` concept in events with per-session head pointers (`SessionID` $\rightarrow$ `HeadNodeID`).
   - `EventNodeSaved` announces node creation to the graph without altering other clients' active viewports unless explicitly pinned to follow a specific session.
2. **Branch-Scoped Compaction (Localized Supernodes)**:
   - Supernodes must behave like Git squash merges rather than destructive global rewires.
   - Shared historical nodes remain reachable as long as any active session head descends from them. Compactions create branch-local supernodes that do not orphan concurrent sibling branches.
3. **Branch-Aware Workspace Sandboxing via Git Worktrees**:
   - Instead of running tools against a single shared checkout, the daemon provisions an isolated Git worktree per active branch or session (e.g. `.please/worktrees/<session-id>/`).
   - Detailed in the [Git Worktree Lifecycle & Mechanics](#git-worktree-lifecycle--mechanics) section below.
4. **Provider Multiplexing**:
   - Implement an engine request queue that arbitrates streaming slots and manages inference context windows per session.

---

### Git Worktree Lifecycle & Mechanics

#### What is a Git Worktree?
In standard Git usage, a repository has a single working tree (the checkout directory) connected to `.git`. Switching branches (`git checkout`) rewrites files on disk in-place. If two concurrent processes attempt to modify or test code on different branches in the same directory, they immediately overwrite each other's work and fail.

**Git Worktrees (`git worktree`)** allow a single repository to have **multiple checkout directories simultaneously attached to the same `.git` object store**.
* **Zero Cloning / Shared Object Store**: Worktrees do not clone the repo or duplicate git history. All commits, blobs, and trees live in the root `.git` folder. Each worktree simply gets a tiny `.git` file with a pointer back to `.git/worktrees/<name>/`.
* **Independent `HEAD` and Index**: Each worktree has its own branch checkout, its own staging index, and its own physical file tree on disk.
* **Instantaneous**: Creating a worktree takes milliseconds because Git merely checks out files locally without network or packing overhead.

#### Location Strategy: Out-of-Tree in Application Support

A critical design choice is **where** ephemeral worktrees reside on the filesystem.

Nesting worktrees inside the project root (e.g. `.please/worktrees/`) introduces significant hazards:
* **Repository & Tooling Pollution**: Language servers (gopls, rust-analyzer, tsserver), IDE file watchers, linters, and recursive grep tools will traverse and index duplicate copies of the entire codebase.
* **Git Status Contamination**: Unless meticulously guarded with `.gitignore` or `.git/info/exclude`, local status checks show hundreds of untracked files.

**Decision for Worktree Storage**: Store all ephemeral session worktrees **out-of-tree** in the application data directory already resolved by `GetConfigDir()` in [internal/engine/config.go](file:///Users/bart/Code/please/internal/engine/config.go#L193) (`os.UserConfigDir()`).

* On macOS: `~/Library/Application Support/please/worktrees/<repo-hash>/<session-id>/`
* On Linux: `~/.config/please/worktrees/<repo-hash>/<session-id>/` (or `$XDG_DATA_HOME`)
* Overridable via `PLEASE_CONFIG_DIR`

#### Proposed Directory Hierarchy

```
[Operator's Workspace - 100% Unmodified & Pure]
/Users/bart/Code/please/
├── .git/
│   └── worktrees/
│       ├── session-alpha/             <-- Git administrative metadata
│       └── session-beta/
├── cmd/
└── internal/

[System Data Directory - Derived via GetConfigDir()]
~/Library/Application Support/please/
├── certs/
├── config.json
├── history.sqlite
└── worktrees/
    └── please-7a8b9c/                 <-- Namespaced by repo name + path hash
        ├── session-alpha/             <-- Isolated checkout on 'please/session-alpha'
        │   ├── .git                   <-- Text pointer back to main .git/worktrees
        │   ├── cmd/
        │   ├── internal/
        │   └── go.mod
        └── session-beta/              <-- Isolated checkout on 'please/session-beta'
            ├── cmd/
            ├── internal/
            └── go.mod
```

#### Lifecycle & Workflow

1. **Session Provisioning**:
   When a client requests branch isolation, the daemon resolves the out-of-tree path and initializes the worktree:
   ```bash
   TARGET_DIR="$HOME/Library/Application Support/please/worktrees/please-7a8b9c/session-alpha"
   git worktree add -b please/session-alpha "$TARGET_DIR" HEAD
   ```
2. **Dynamic Tool Execution Scoping**:
   The daemon binds `TARGET_DIR` as the execution root for all tool invocations (`tools.ExecTool`, `tools.FileWrite`, `tools.SearchTool`) associated with that session.
   * Client Alpha can modify code, trigger compile errors, and run test suites in isolation.
   * The operator's main repo and other connected sessions remain completely untouched.
3. **Workspace Path Virtualization**:
   To prevent sandbox violations when prompts reference absolute repository paths:
   * [internal/tools/sandbox.go](file:///Users/bart/Code/please/internal/tools/sandbox.go)'s `ValidateSafePath` remaps paths prefixed with the repository root to the corresponding path within the active worktree root.
   * DAG nodes, telemetry envelopes, and supernodes strictly persist **repo-relative paths** (`internal/tools/exec.go`), ensuring contextual resonance maps remain portable across branch merges.
4. **Session Termination & Garbage Collection**:
   When the session finishes, merges back, or is discarded:
   ```bash
   git worktree remove --force "$TARGET_DIR"
   git branch -D please/session-alpha
   ```
   If a daemon terminates unexpectedly, recovery runs `git worktree prune`, safely removing stale worktree references from `.git` without risking repository data.

---

## Decision

We formally recognize the multi-client concurrency hazard and adopt a two-phase roadmap:

1. **Phase 1 (Immediate / Stabilization)**:
   * **Decouple TUI Camera Control**: Remove automatic `m.CurrentID = lastID` camera snapping on passive clients. Remote events will update the background graph and map view without stealing focus from the active user's viewport.
   * **Explicit Session Identification**: Introduce a lightweight `session_id` header in `please connect` handshakes, laying the groundwork for per-connection cursor tracking.
   * **Compaction Guards**: Prevent pruning of nodes that have active child branches or open session attachments.

2. **Phase 2 (Evolutionary Roadmap)**:
   * Evaluate Git worktree isolation for tool execution if multi-agent or multiplayer pair-programming becomes a core priority.
   * Formalize branch-isolated supernodes where compaction is strictly scoped to the active trajectory path.

---

## Consequences

### Positive
* Clarifies the design boundary of `please serve`: moves the system from implicit single-operator assumptions to explicit concurrency safety.
* Prevents data loss, viewport jumping, and context disorientation during multi-device or multi-client usage.
* Prepares the Go engine foundation for the multi-device Apple/iPad jump outlined in [ADR 004](004-swift-apple-ecosystem-evolution.md).

### Negative
* True multi-session workspace isolation (Git worktrees, per-session sandboxes) introduces disk overhead and state management complexity.
* Branch-aware compaction algorithms require stricter DAG traversal logic to ensure historical nodes are not prematurely collected.
