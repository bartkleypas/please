---
type: Concept
title: "Server & API Package"
description: "HTTP, REST v1, and Server-Sent Events (SSE) engine daemon package providing graph CRUD, streaming chat generation, 20-year internal PKI certificate minting, Bearer authentication, and embedded web visualization."
tags:
  - please
  - server
  - sse
  - api
  - tls
  - index
timestamp: "2026-08-21T13:25:00-07:00"
---

# Server & API Package

The `server` package provides the headless daemon, REST v1 API, and real-time Server-Sent Events (SSE) streaming infrastructure for `please`. It serves as the authoritative engine and backend layer for external clients (macOS/iPadOS native apps, Web dashboards, CLI automation, and TUI).

## 🗺️ Components & Source Files

*   [server.go](server.go) - Core `Server` lifecycle (`StartWithHost`, `StartTLS`, `Stop`), CORS and Bearer auth middlewares, and REST API v1 route controllers.
*   [stream.go](stream.go) - Server-Sent Events (`SSE`) multiplexer and handler (`POST /api/v1/chat/stream`), managing multi-turn agent tool execution loops on the host machine.
*   [cert.go](cert.go) - Self-contained 20-year (7,300 days) ECDSA Root CA and Leaf Server Certificate generator with Subject Alternative Names (SANs) for secure local network encryption.
*   [server_test.go](server_test.go) - Comprehensive unit and integration test suite covering cert generation, auth rejection/acceptance, REST v1 endpoints, and SSE stream flushing.
*   [assets/](assets/) - Embedded web visualization single-page application (`assets/index.html`).

---

## 📡 REST API v1 Specification

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Service healthcheck, version, provider, and model information. |
| `GET` | `/api/v1/graph` | Synchronizes storage and returns complete serialized DAG topology. |
| `GET` | `/api/v1/nodes/{id}` | Retrieves full node metadata, observations, and timestamps. |
| `POST` | `/api/v1/nodes` | Inserts a new node into the graph (system, user, assistant, tool). |
| `DELETE` | `/api/v1/branches/{id}` | Recursively soft-prunes a conversation branch. |
| `POST` | `/api/v1/nodes/{id}/prune` | Alternative POST endpoint for branch pruning. |
| `POST` | `/api/v1/supernodes` | Summarizes a range of node IDs into a compacted Supernode. |
| `POST` | `/api/v1/gc` | Permanently deletes soft-pruned records from storage. |
| `GET` | `/api/v1/tools` | Lists registered workspace tools and JSON schemas. |
| `POST` | `/api/v1/chat/stream` | Initiates real-time SSE streaming dialogue turn. |

---

## ⚡ Server-Sent Events (SSE) Protocol (`/api/v1/chat/stream`)

The streaming endpoint emits typed SSE frames:

*   `event: thought` — Real-time reasoning tokens emitted by thinking models (e.g. DeepSeek-R1, Gemma 4).
*   `event: token` — Dialogue text tokens streamed directly for stage canvas presentation.
*   `event: tool_call` — Emitted when the model triggers a host workspace tool.
*   `event: tool_result` — Emitted after the host tool completes execution.
*   `event: node_complete` — Emitted when the turn finishes and is persisted to SQLite.
*   `event: error` — Error reporting.
