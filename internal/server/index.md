---
type: Concept
title: "Package internal/server Index"
description: "HTTP, REST v1, and Server-Sent Events (SSE) engine daemon package providing graph CRUD, multi-session actor mailboxes, streaming chat generation, 20-year PKI certificates, and embedded web visualization."
tags:
  - please
  - server
  - sse
  - api
  - tls
  - index
timestamp: "2026-10-11T10:23:00-07:00"
---

# Package internal/server Index (`internal/server`)

The `internal/server` package (Layer 3: Presentation & Daemon) provides the headless daemon (`please serve`), REST v1 API, per-session actor mailboxes, and real-time Server-Sent Events (SSE) streaming infrastructure for `Please`. It serves as the authoritative multi-session backend for external clients ([please-swift](../../../please-swift/index.md), `please connect`, Web UI, and CLI scripts). See the [Daemon Protocol Specification](../../docs/daemon_protocol_spec.md) for full wire schemas.

## 🗺️ Components & Source Files

* [server.go](server.go) - Core `Server` lifecycle (`StartWithHost`, `StartTLS`, `Stop`), CORS and Bearer auth middlewares, session actor registry (`getOrCreateActor`), and REST API v1 route controllers.
* [stream.go](stream.go) - Server-Sent Events (`SSE`) pump (`POST /api/v1/chat/stream`), dispatching turns to the target `SessionActor` and streaming `HarnessEvent` frames to the HTTP client.
* [session_actor.go](session_actor.go) - Implements `SessionActor` ([ADR-007](../../decisions/007-multi-session-daemon-branch-concurrency.md)). Provides each named session (`main`, `alpha`, `sub_<id>`) with a dedicated, single-threaded FIFO mailbox and optional isolated Git worktree (`worktree.Manager`), executing turns via `engine.SessionHarness` without cross-session race conditions.
* [bus.go](bus.go) - Internal publish/subscribe `EventBus` broadcasting graph mutations (`node_saved`, `branch_pruned`, `supernode_created`) and session activity to connected SSE listeners (`/api/v1/events`).
* [cert.go](cert.go) - Self-contained 20-year (7,300 days) ECDSA Root CA and Leaf Server Certificate generator (`Generate20YearCerts`) with Subject Alternative Names (`localhost`, `127.0.0.1`, `please.local`).
* [server_test.go](server_test.go) & [bus_test.go](bus_test.go) - Unit and integration test suites covering cert generation, Bearer auth, multi-session actor isolation, REST v1 endpoints, and SSE streaming.
* [assets/](assets/) - Embedded web visualization single-page application (`assets/index.html`).

---

## 📡 REST API v1 Specification

| Method | Route | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/health` | Service healthcheck, version, provider, and model information. |
| `GET` | `/api/v1/graph?session={id}` | Synchronizes storage and returns serialized DAG topology for the session. |
| `GET` | `/api/v1/nodes/{id}` | Retrieves full node metadata, observations, and timestamps. |
| `POST` | `/api/v1/nodes` | Inserts or updates a node in the graph (`system`, `user`, `assistant`, `tool`, `summary`). |
| `DELETE` | `/api/v1/branches/{id}` | Recursively soft-prunes a conversation branch (guarded by active session heads). |
| `POST` | `/api/v1/nodes/{id}/prune` | Alternative POST endpoint for branch pruning. |
| `POST` | `/api/v1/supernodes` | Summarizes a range of node IDs into a compacted `RoleSummary` Supernode. |
| `POST` | `/api/v1/gc` | Permanently deletes soft-pruned records from storage. |
| `GET` | `/api/v1/sessions` | Lists all named sessions and their active `head_node_id` pointers. |
| `GET` | `/api/v1/sessions/{id}` | Retrieves the active head node ID for a specific named session. |
| `POST` | `/api/v1/sessions` | Updates or creates a named session head pointer. |
| `GET` | `/api/v1/tools` | Lists registered workspace tools and JSON schemas. |
| `GET` | `/api/v1/events` | Persistent SSE event bus stream for live graph topology updates. |
| `POST` | `/api/v1/chat/stream?session={id}` | Initiates real-time SSE streaming turn dispatched through the session's `SessionActor`. |

---

## ⚡ Server-Sent Events (SSE) Protocol (`/api/v1/chat/stream`)

The streaming endpoint emits typed SSE frames mapped directly from `engine.HarnessEvent`:
* `event: thought` — Real-time reasoning tokens emitted by thinking models (e.g. DeepSeek-R1, Gemma 4).
* `event: token` — Dialogue text tokens streamed for viewport presentation.
* `event: tool_call` — Emitted when the model invokes a host workspace tool.
* `event: tool_result` — Emitted after the host tool completes execution (with `SmartReceipt` metadata).
* `event: node_complete` — Emitted when the turn finishes and is persisted to SQLite.
* `event: error` — Terminal error reporting.
