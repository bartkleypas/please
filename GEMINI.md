---
type: Concept
title: "Please: Agent Bootstrap Memory"
description: "Immediate orientation anchor for developers and AI agents waking up in the please project."
tags:
  - please
  - go
  - bootstrap
timestamp: "2026-10-11T10:21:00-07:00"
---

# Please: Agent Bootstrap Memory

> [!IMPORTANT]
> **Attention Agents / Context Windows:** This file serves as the system "bootstrap" memory. Read this first to understand the *immediate* state of the project, its 4-tier package architecture, memory hierarchy, and active conventions.

---

## 🗺️ Project Navigation Hub

For detailed and authoritative documentation, consult these dedicated files instead of relying on this bootstrap anchor:
* [Project Index](index.md) - The central directory of packages, concepts, and specifications.
* [Technical README](README.md) - Features list, CLI examples, setup, TUI controls table, and testing instructions.
* [v0.3.0 Roadmap](docs/roadmap_v0.3.0.md) - Completed strategic architecture for execution lifecycle convergence, three-tier memory, pluggable context shaping, and delegated multi-agent topologies.
* [Daemon Protocol Spec](docs/daemon_protocol_spec.md) - Authoritative REST v1 and SSE streaming wire protocol specification.
* [Context Shaping & Resonance Spec](docs/context_resonance.md) - Cache-stable `ContextShaper` projections (`SigmoidShaper`, `WindowShaper`, `ResonanceShaper`) and `SmartReceipt` paging.
* [Natural Pacing Spec](docs/natural_pacing.md) - Stream buffering and punctuation-sensitive pacing rules.
* [Architecture Decisions (ADRs)](decisions/index.md) - Authoritative record of all 22 architectural decisions (`ADR-001` through `ADR-022`).
* [Change Log](log.md) - Chronological ledger of all modifications.

---

## 🏗️ Active Design Context & Baseline (`v0.3.1`)

* **Current Baseline (`v0.3.1`)**: All four pillars of the [v0.3.0 Roadmap](docs/roadmap_v0.3.0.md) and `v0.3.1` ergonomic refinements are implemented and verified:
  1. **Canonical Execution Lifecycle ([ADR-019](decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md))**: `SessionHarness` ([`internal/engine/harness.go`](internal/engine/harness.go)) is the single authoritative execution runner across standalone TUI (`LocalHarnessProvider`), headless daemon (`SessionActor`), and IDE ACP (`please acp`).
  2. **Cache-Stable Context Shaping ([ADR-020](decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md))**: Pure read-only `ContextShaper` projections (`sigmoid` default, `window`, `exponential`) in [`internal/engine/shaper.go`](internal/engine/shaper.go) driven strictly by topological step distance ($\Delta d$) and token fill ratio—zero wall-clock time decay, preserving monotonic prefix invariance for KV-cache reuse.
  3. **Three-Tier Memory & Smart Receipts ([ADR-014](decisions/014-persistent-agent-memory-and-cybernetic-recall.md), [ADR-021](decisions/021-tiered-sensory-storage-and-observation-compaction.md))**:
     * **L1 (Working Context Window)**: Ephemeral in-flight prompt buffer shaped by `ContextShaper`.
     * **L2 (Sensory Observation Vault)**: Out-of-band gzipped telemetry in SQLite `observation_blobs` paired with deterministic in-DAG `SmartReceipt` (`obs_<hash>`) and on-demand paging via `inspect_receipt`.
     * **L3 (Cybernetic Semantic Store)**: Durable FTS5-indexed knowledge in SQLite `memories` managed via `memory_store`, `memory_recall`, `memory_delete`, and `memory_diagnose`.
  4. **Native Delegated Multi-Agent Topologies ([ADR-022](decisions/022-native-delegated-multi-agent-topologies.md))**: First-class sub-session delegation (`spawn_subagent`, `inspect_subagent`, `reconcile_subagent`) with isolated out-of-tree Git worktrees, `target_dir` nested repository support, and synthesized perception shielding.
* **Clients & Surfaces**: Standalone Go TUI (`please`), Headless Daemon (`please serve`), Connected Go TUI (`please connect`), IDE Agent Client Protocol (`please acp` / `please-acp`), and the native Apple companion client ([please-swift](../please-swift/index.md)).

---

## 🏛️ Four-Tier Package Stratification ([ADR-018](decisions/018-package-stratification-and-domain-decoupling.md))

When adding or modifying Go code, strictly respect the downward-only acyclic import hierarchy:

| Tier | Role | Packages | Import Rule |
| :--- | :--- | :--- | :--- |
| **Layer 3** | **Presentation & Entrypoints** | `cmd/please`, `internal/tui`, `internal/server`, `internal/acp` | May import Layers 2, 1, and 0. |
| **Layer 2** | **Core Orchestration** | `internal/engine` (`SessionHarness`, `Manager`, `ContextShaper`, delegation) | May import Layers 1 and 0. |
| **Layer 1** | **Infrastructure & Capabilities** | `internal/graph`, `internal/storage`, `internal/tools`, `internal/providers`, `internal/config`, `internal/worktree` | May import Layer 0 (and minimal justified Layer 1 peers). `providers` must **never** import `tools`. |
| **Layer 0** | **Core Domain Primitives** | `internal/domain` (`Message`, `Role`, `ToolCall`, `ToolObservation`, `SmartReceipt`, `ModelOptions`) | **Zero internal imports** (Go stdlib only). |

---

## 💻 Key Developer Reminders

* **DAG Navigation**: The "current" view in the app is always a leaf node in a DAG (tracked per session in SQLite `sessions`). Reconstruct linear history by traversing parent pointers up to the root (`Graph.GetPath(nodeID)`).
* **TUI ViewStack Architecture ([ADR-017](decisions/017-tui-viewstack-and-hierarchical-focus-architecture.md))**: The Bubble Tea TUI routes focus and rendering through `ViewStack` ([`internal/tui/view_stack.go`](internal/tui/view_stack.go)) and discrete `Layer` implementations (`ChatLayer`, `MapLayer`, `MemoriesLayer`, `TextLayer`, `ConfirmOverlay`). Never add ad-hoc boolean modal flags to `tui.Model`.
* **Workspace Hermeticity ([ADR-016](decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md))**: Project-local state lives in `.please/` (`config.json`, `vault.db`), falling back to the global anchor `~/.please/`.
* **Testing Discipline ([ADR-015](decisions/015-living-scenarios-and-test-subsystem-decomposition.md))**: Always run fast hermetic tests (`go test -count=1 ./...` and `make build`) for automated verification (~2s). End-to-end multi-turn harness and delegation flows are verified deterministically via Living Scenarios in `internal/engine/scenarios_test.go` and `test/scenarios/`.
* **Tool Contracts as Sensory Input**: Tools must return concrete telemetry (paths, byte sizes, line counts, receipt IDs) and descriptive error messages with actionable recovery hints to prevent model retry loops.
* **Built-in Diagnostics**: Use `please inspect`, `please context`, `please memory`, and `please vault` to diagnose live SQLite vaults without writing scratch code.

---

## 🔍 Diagnostic & Inspection Tools

* **Inspect Node, Receipts & Lineage Scorecard**:
    ```bash
    please inspect [node-id] [-v /path/to/vault.db] [-c config.json]
    ```
    *Omitting `[node-id]` automatically targets the active session head or latest leaf node.*
    Prints node metadata, exact tool calls and observations (with `SmartReceipt` badges and byte counts), and an ASCII table of the path to root showing topological distance, token costs, shaper scores, and fidelity tiers.

* **Inspect LLM Reconstructed Context**:
    ```bash
    please context [node-id] [-v /path/to/vault.db] [-c config.json] [--json]
    ```
    Reconstructs and displays the exact linear prompt sequence (`[]domain.Message`) produced by the active `ContextShaper` for that turn, including system prompt overlays, `SmartReceipt` compactions, and ambient telemetry envelopes. Pass `--json` to output clean JSON suitable for piping to `jq`.

* **Inspect & Manage Cybernetic Memories (L3)**:
    ```bash
    please memory [list|inspect <key>|diagnose|prune] [-v /path/to/vault.db]
    ```
    Inspects persistent semantic memories, access counts, confidence scores, and FTS5 vault health.

* **Workspace Initialization & Vault Maintenance**:
    ```bash
    please init [-g] [-w /path/to/workspace]
    please vault [decrypt|clean] [-v /path/to/vault.db]
    ```

---

## 🗺️ Next Up (Roadmap)

- [ ] **Daemon Observability & Graph Visualization**:
  - [ ] Web-based D3 graph visualizer served directly from `please serve` (`/visualize`).
  - [ ] Real-time SSE event bus streaming node creation, tool execution, and branch forks to web clients.
- [x] **v0.3.0 Unified Execution Lifecycle & Three-Tier Memory Architecture**:
  - [x] Pillar 1: Lifecycle Convergence onto `SessionHarness` & Interactive `PermissionGate` ([ADR-019](decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md)).
  - [x] Pillar 2: Cache-Stable `ContextShaper` (`sigmoid`, `window`, `exponential`) & Monotonic Prefix Invariance ([ADR-020](decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md)).
  - [x] Pillar 3: Tiered Sensory Storage, `observation_blobs`, `SmartReceipt`, and `inspect_receipt` paging ([ADR-021](decisions/021-tiered-sensory-storage-and-observation-compaction.md)).
  - [x] Pillar 4: Native Delegated Multi-Agent Topologies (`spawn_subagent`, `inspect_subagent`, `reconcile_subagent`) & nested repo worktree isolation ([ADR-022](decisions/022-native-delegated-multi-agent-topologies.md)).
