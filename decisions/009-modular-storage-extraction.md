---
type: Decision
title: "ADR 009: Modular Storage & Vault Subsystem Extraction"
description: "Extraction of SQLite WAL persistence, JSONL fallback, remote daemon storage proxying, and AES-GCM vault encryption from internal/engine into a dedicated internal/storage package."
tags:
  - please
  - architecture
  - adr
  - storage
  - sqlite
  - decoupling
timestamp: "2026-09-07T18:36:00-07:00"
---

# ADR 009: Modular Storage & Vault Subsystem Extraction

## Status

Accepted

## Context

`internal/engine` currently maintains all persistence drivers and cryptographic vault operations:
* **`storage.go`** (501 lines): Core `Storage` interface, `SQLiteStorage` (WAL mode, transactions, schema migrations, garbage collection), and `JSONLStorage` fallback.
* **`storage_remote.go`** (200 lines): `RemoteDaemonStorage` proxying RPC mutations over HTTP/SSE with session identification (`X-Please-Session-ID`).
* **`crypto.go`** (91 lines): AES-256-GCM authenticated encryption at rest, PBKDF2 key derivation, and vault passphrase handling.
* Corresponding unit and regression test suites (`storage_test.go`, `storage_remote_test.go`, `crypto_test.go`).

### The Architectural Conflict
Persistence is an infrastructural concern, while `internal/engine` is fundamentally a cognitive orchestration engine (handling context resonance, stream pacing, tool execution, and supernode compaction).

Coupling storage drivers directly inside `engine` causes several drawbacks:
1. **Testing Overhead**: Unit testing the engine requires instantiating SQLite database files or maintaining mock storage drivers directly within the engine package.
2. **Alternative Backends**: Experimenting with alternative storage engines (e.g. PostgreSQL, Redis, Apple CloudKit for iPadOS as planned in [ADR 004](004-swift-apple-ecosystem-evolution.md)) would further bloat `internal/engine`.
3. **Daemon Storage Separation**: `RemoteDaemonStorage` acts as a network client, whereas `SQLiteStorage` acts as an embedded database driver. Grouping them into a dedicated storage subsystem establishes a unified persistence interface.

---

## Decision

We will extract all persistence engines, storage interfaces, remote proxies, and cryptographic vault handlers from `internal/engine` into a dedicated package: **`internal/storage`**.

### Target Package Structure

```
internal/storage/
├── storage.go        # Storage interface definition & common helpers
├── sqlite.go         # SQLiteStorage (WAL mode, schema migrations, vacuum/GC)
├── jsonl.go          # JSONLStorage flat-file fallback
├── remote.go         # RemoteDaemonStorage client proxy & applyHeaders
├── crypto.go         # AES-256-GCM vault encryption & key derivation
├── storage_test.go   # SQLite & JSONL unit tests
├── remote_test.go    # HTTP RPC mock unit tests
└── crypto_test.go    # Encryption/decryption roundtrip tests
```

### Core Interface Contract

The `storage.Storage` interface remains the contract consumed by `Manager` in `internal/engine`:

```go
package storage

type Storage interface {
    SaveNode(node *graph.Node) error
    LoadGraph() (*graph.Graph, string, error)
    GarbageCollect() (int64, error)
    UpdateNodeMetadata(node *graph.Node) error
    UpdateNodeParentID(nodeID, newParentID string) error
    UpdateNodeObservations(nodeID string, obs []providers.ToolObservation) error
}
```

### Engine Integration via Type Aliases

`internal/engine` will preserve full backward compatibility by re-exporting the storage contracts:

```go
package engine

import "github.com/bartkleypas/please/internal/storage"

type Storage = storage.Storage
type SQLiteStorage = storage.SQLiteStorage
type JSONLStorage = storage.JSONLStorage
type RemoteDaemonStorage = storage.RemoteDaemonStorage

var NewSQLiteStorage = storage.NewSQLiteStorage
var NewJSONLStorage = storage.NewJSONLStorage
var NewRemoteDaemonStorage = storage.NewRemoteDaemonStorage
var EncryptVault = storage.EncryptVault
var DecryptVault = storage.DecryptVault
```

---

## Consequences

### Positive
* **Decoupled Persistence**: Completely frees `internal/engine` from database drivers (`modernc.org/sqlite`) and cryptographic primitives.
* **Pluggable Backends**: Simplifies adding future backends, including CloudKit sync for iPadOS ([ADR 004](004-swift-apple-ecosystem-evolution.md)) or ephemeral in-memory storage for headless batch evaluation.
* **Isolated Testing**: Database transaction and schema migration tests run in their own domain without executing LLM orchestrations.

### Negative
* Requires coordinating imports between `internal/graph` (node/graph definitions) and `internal/storage`.
