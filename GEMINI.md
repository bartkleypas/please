---
type: Concept
title: "Please: Agent Bootstrap Memory"
description: "Immediate orientation anchor for developers and AI agents waking up in the please project."
tags:
  - please
  - go
  - bootstrap
timestamp: "2026-07-09T14:35:00-07:00"
---

# Please: Agent Bootstrap Memory

> [!IMPORTANT]
> **Attention Agents / Context Windows:** This file serves as the system "bootstrap" memory. Read this first to understand the *immediate* state of the project, open debates, and active tasks.

---

## 🗺️ Project Navigation Hub

For detailed and authoritative documentation, consult these dedicated files instead of relying on this bootstrap anchor:
*   [Project Index](index.md) - The central directory of packages, concepts, and specifications.
*   [Technical README](README.md) - Features list, CLI examples, setup, TUI controls table, and testing instructions.
*   [Daemon Protocol Spec](docs/daemon_protocol_spec.md) - Authoritative REST v1 and SSE streaming wire protocol specification.
*   [Context Resonance Spec](context_resonance.md) - Math formula and scoring mechanics for token decay.
*   [Natural Pacing Spec](natural_pacing.md) - Stream buffering and punctuation-sensitive pacing rules.
*   [Architecture Decisions (ADRs)](decisions/index.md) - Historical record of TUI framework, SQLite storage, and visualization server.
*   [Change Log](log.md) - Chronological ledger of all modifications.

---

## 🏗️ Active Design Context & Focus

*   **Current Focus**: Engine daemon (`please serve`), REST v1 / SSE streaming API, remote client protocol (`please connect`), and resilient workspace tool execution.
*   **Go Baseline**: Bubble Tea TUI, DAG conversation graph, SQLite WAL storage, and 20-year internal PKI certs (`please cert generate`) stabilized ([v0.1.10](log.md)).
*   **Clients**: Standalone Go TUI (`please`), Connected Go TUI (`please connect`), and the external native Apple client ([please-swift](../please-swift/index.md)).

---

## ⚖️ Active Debates

*   *There are currently no active debates.* Architecture decisions for SQLite persistence, TUI state machines, and daemon streaming protocols are resolved in [decisions/](decisions/index.md).

---

## 💻 Key Developer Reminders

To maintain the architectural conventions of the `please` codebase during edits:
*   **DAG Navigation**: Remember that the "current" view in the app is always a leaf node in a DAG. Reconstruct linear history by traversing parent pointers up to the root.
*   **LLM Provider Abstraction**: Maintain strict protocol boundaries (`LLMProvider`) so local Ollama backends, OpenAI-compatible cloud/self-hosted endpoints, and remote `please serve` daemons swap seamlessly.
*   **Testing Discipline**: Always run fast hermetic tests (`go test -count=1 ./...` and `make build`) for automated verification (~2s). Treat `PLEASE_LIVE_FIRE=1` (`make test-livefire`) as **Manual / On-Demand Integration Testing** triggered only when explicitly evaluating real local LLM inference.
*   **Tool Contracts as Sensory Input**: Tools must return concrete telemetry (paths, byte sizes, line counts) and descriptive error messages with actionable hints to prevent model retry loops.

---

## 🗺️ Next Up (Roadmap)

- [ ] **Daemon Observability & Graph Visualization**:
  - [ ] Web-based D3 graph visualizer served directly from `please serve` (`/visualize`).
  - [ ] Real-time SSE event bus streaming node creation, tool execution, and branch forks to web clients.
- [ ] **Multi-Turn Tool Orchestration**:
  - [x] Resilient write telemetry, `overwrite: bool` support, and offset-based `read_file` pagination.
  - [x] Add "append_file" tool with line-boundary hygiene to models tool kit.
  - [x] Reduce/reuse/recycle on the redundant commands (eg:`patch_file`, `edit_file`, `search_and_replace`, etc).
  - [x] Admit that yes, technically adding an `append_file` tool first was a classic off by one error. This is an easy one to check off.
  - [x] Fix long standing tool order bug causing KV cache invalidation.
  - [x] Add explicit Categorization on Tool for `read/write/execute` ToolCategory types (security bridge).
  - [x] Use new tool categorization for explicit boundries in SandboxPolicy enforcement and rule guidlines.
- [ ] **Dynamic Root/System Prompt/Node**:
  - [ ] Eval of impact to model context views
- [x] **Cross-Platform & Client Interop**:
  - [x] Schema v2 config migration for clean client/server separation.
  - [x] 20-year internal Root CA and Server TLS generation for local network pairing.
  - [x] Stable REST / SSE contracts for native companion clients ([daemon_protocol_spec.md](docs/daemon_protocol_spec.md)).
