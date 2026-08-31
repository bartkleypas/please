---
type: Reference
title: "Please Daemon Wire Protocol & Client Interoperability Specification"
description: "Authoritative specification for REST v1 and Server-Sent Events (SSE) streaming protocols exposed by please serve for multi-platform clients."
tags:
  - please
  - daemon
  - sse
  - rest
  - protocol
  - spec
  - interop
timestamp: "2026-08-31T12:25:00-07:00"
---

# 🦉 Please Daemon Wire Protocol & Client Interoperability Specification

## 1. Overview & Architecture

The `please` application architecture decouples the core conversational engine from its presentation interfaces. The headless daemon (`please serve`) acts as the authoritative host process: managing conversation Directed Acyclic Graphs (DAGs), executing workspace tools, persisting history to SQLite, and arbitrating local/cloud LLM inference.

Clients connect to the daemon over standard HTTP REST v1 and Server-Sent Events (SSE) streaming protocols. This decouples platform-specific user experiences (such as terminal TUIs, native macOS/iPadOS SwiftUI canvases, or web visualizers) from the underlying Go runtime.

```
┌──────────────────────────────────────────────────────────────────────────┐
│                             Client Ecosystem                             │
│                                                                          │
│  ┌──────────────────────┐  ┌──────────────────────┐  ┌────────────────┐  │
│  │   Go Connected TUI   │  │     Please Swift     │  │  Custom / Muc  │  │
│  │   (please connect)   │  │   (macOS / iPadOS)   │  │  (Groovy/Web)  │  │
│  └──────────┬───────────┘  └──────────┬───────────┘  └────────┬───────┘  │
└─────────────┼─────────────────────────┼───────────────────────┼──────────┘
              │                         │                       │
              └────────────┬────────────┴───────────────────────┘
                           │ HTTP REST v1 & Server-Sent Events (SSE)
                           │ Bearer Token + 20-Year Internal PKI TLS
                           ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                        Headless Engine Daemon                            │
│                            (please serve)                                │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │ REST v1 API Controllers & SSE Streaming Multiplexer                │  │
│  └─────────────────────────────────┬──────────────────────────────────┘  │
│                                    │                                     │
│     ┌──────────────────────────────┼──────────────────────────────┐      │
│     ▼                              ▼                              ▼      │
│ ┌───────────────┐          ┌───────────────┐              ┌────────────┐ │
│ │  DAG Manager  │          │ Tool Sandbox  │              │    LLM     │ │
│ │  (SQLite WAL) │          │  (Telemetry)  │              │ Providers  │ │
│ └───────────────┘          └───────────────┘              └────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Handshake, Security & Trust

### 2.1 20-Year Internal PKI TLS
To provide encrypted communication without cloud dependencies or expiring certificate hassles across local mesh networks (e.g. Tailscale, LAN, localhost), `please cert generate` mints an internal ECDSA Root Certificate Authority (CA) and Server Leaf certificate valid for 7,300 days (20 years).

* **Subject Alternative Names (SANs)**: Certificates include `localhost`, `127.0.0.1`, `::1`, and local hostnames (`*.local`).
* **Client Trust Delegate Pattern**: External clients should not modify the system macOS Keychain. Instead, clients implement custom TLS validation:
  - **Go Reference**: Load the minted `ca.crt` into an `x509.CertPool` in `http.Transport.TLSClientConfig`.
  - **Swift Reference**: Implement `URLSessionDelegate.urlSession(_:didReceive:completionHandler:)` verifying `challenge.protectionSpace.serverTrust` for `localhost`, `127.0.0.1`, or `*.local`.

### 2.2 Authentication
When enabled via configuration or `--auth-token`, the daemon requires HTTP Bearer token authentication on all `/api/v1/*` routes (except `/api/v1/health` when unauthenticated healthchecks are permitted):

```http
Authorization: Bearer <auth-token>
```

The daemon verifies tokens using constant-time comparison to prevent timing attacks. Missing or mismatched tokens return `HTTP 401 Unauthorized`.

---

## 3. Canonical Data Models

### 3.1 Strict RFC 3339 Timestamps
All timestamps across JSON payloads MUST follow strict **RFC 3339** format in UTC or with explicit timezone offsets:
```json
"2026-08-31T12:00:00Z"
```
Clients must format outgoing timestamps using RFC 3339 and support ISO8601/RFC3339 decoding.

### 3.2 Role Enum
* `system` — System instructions and persona definition.
* `user` — Human input turns.
* `assistant` — Spoken model replies.
* `tool` — Tool observation results.
* `summary` — Compaction supernodes summarizing conversational branches.

### 3.3 Node Schema
```json
{
  "id": "0191a3e4-5678-7000-8000-000000000001",
  "parent_id": "0191a3e4-1234-7000-8000-000000000000",
  "role": "assistant",
  "content": "I have created the requested file.",
  "thought": "The user asked for a configuration script...",
  "images": [],
  "tool_calls": [
    {
      "id": "call_12345",
      "type": "function",
      "function": {
        "name": "write_file",
        "arguments": "{\"path\":\"config.json\",\"content\":\"{}\"}"
      }
    }
  ],
  "observations": [
    {
      "tool": "write_file",
      "result": "file 'config.json' created successfully (2 bytes, 1 lines)"
    }
  ],
  "metadata": {
    "signat": "🦉📚",
    "model": "gemma-4-31b"
  },
  "created_at": "2026-08-31T12:00:00Z"
}
```

---

## 4. REST v1 Endpoints & Resources

| Method | Endpoint | Description | Response / Payload |
|---|---|---|---|
| `GET` | `/api/v1/health` | Daemon liveness probe | `{"status":"ok","version":"v0.1.10"}` |
| `GET` | `/api/v1/status` | Full engine diagnostics | Version, active model, provider, loaded node count |
| `GET` | `/api/v1/nodes` | List all graph nodes | Array of `Node` objects `[ {...}, {...} ]` |
| `GET` | `/api/v1/nodes/{id}` | Fetch a single node by ID | Single `Node` object |
| `POST` | `/api/v1/nodes` | Insert node into graph | Body: `Node` object $\rightarrow$ Returns created `Node` |
| `GET` | `/api/v1/path/{id}` | Linear path from root to leaf | Array of `Node` objects ordered root-to-leaf |
| `DELETE`| `/api/v1/branches/{id}`| Soft-prune a branch | `{"pruned_count": 4, "root_id": "..."}` |
| `POST` | `/api/v1/supernodes` | Compact range into Supernode | Body: candidate IDs $\rightarrow$ Returns summary `Node` |
| `POST` | `/api/v1/gc` | Permanently scrub soft-pruned | `{"purged_count": 12}` |
| `GET` | `/api/v1/tools` | List registered host tools | Array of tool definitions with JSON schemas |

---

## 5. Real-Time SSE Stream Protocol (`POST /api/v1/chat/stream`)

The streaming endpoint initiates multi-turn model generation and execution loops on the daemon.

### 5.1 Request Payload
```json
{
  "message": "Please list the files in the current directory.",
  "images": [],
  "parent_id": "0191a3e4-5678-7000-8000-000000000001",
  "role": "user"
}
```

### 5.2 Stream Event Lifecycle
Responses stream over `Content-Type: text/event-stream`. Frames are typed and emitted sequentially:

```
[Stream Open]
  │
  ├─► event: thought         (Zero or more reasoning chunks)
  │   data: {"chunk":"Analyzing request..."}
  │
  ├─► event: tool_call       (Emitted if model triggers a host tool)
  │   data: {"id":"call_1","tool":"list_dir","arguments":"{\"path\":\".\"}"}
  │
  ├─► event: tool_result     (Emitted after host executes tool)
  │   data: {"id":"call_1","tool":"list_dir","output":"file1.go\nfile2.go"}
  │
  ├─► event: token           (Zero or more final spoken dialogue chunks)
  │   data: {"chunk":"Here are "}
  │
  ├─► event: token
  │   data: {"chunk":"the files."}
  │
  └─► event: node_complete   (Turn finalized and committed to SQLite)
      data: {"node_id":"0191a3e4-...","role":"assistant","timestamp":"..."}
[Stream Closed]
```

### 5.3 Event Frame Definitions

#### `event: thought`
Emitted when reasoning models (e.g. DeepSeek-R1, Gemma 4) stream interstitial thinking:
```json
{"chunk": "Checking directory permissions..."}
```

#### `event: token`
Standard dialogue tokens intended for user presentation:
```json
{"chunk": "I found 3 files."}
```

#### `event: tool_call`
Model requested execution of a host tool:
```json
{
  "id": "call_99",
  "tool": "read_file",
  "arguments": "{\"path\":\"main.go\",\"offset\":1,\"limit\":50}"
}
```

#### `event: tool_result`
Host daemon completed tool execution:
```json
{
  "id": "call_99",
  "tool": "read_file",
  "output": "package main...",
  "error": ""
}
```

#### `event: node_complete`
Terminal turn event. Indicates model generation and host tool loops are finished, and the new assistant node has been durably stored in the DAG:
```json
{
  "node_id": "0191a3e4-9999-7000-8000-000000000002",
  "parent_id": "0191a3e4-5678-7000-8000-000000000001",
  "role": "assistant",
  "timestamp": "2026-08-31T12:00:05Z"
}
```

#### `event: error`
Terminal error event emitted if model connection or tool execution suffers an unrecoverable failure:
```json
{"error": "context deadline exceeded while contacting inference provider"}
```

---

## 6. Reference Implementations

* **Go Reference Client (`please connect`)**:
  - Provider: [`internal/engine/remote.go`](../internal/engine/remote.go) (`RemoteDaemonProvider`)
  - Storage Proxy: [`internal/engine/remote.go`](../internal/engine/remote.go) (`RemoteDaemonStorage`)
* **Swift Native Companion (`Please Swift`)**:
  - Daemon Actor: [`please-swift/Sources/PleaseEngine/Client/PleaseDaemonClient.swift`](../../please-swift/Sources/PleaseEngine/Client/PleaseDaemonClient.swift)
  - TLS Delegate: `LocalhostURLSessionDelegate` in `PleaseDaemonClient.swift`
