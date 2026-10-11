---
type: Concept
title: "Package internal/storage Index"
description: "Persistence storage drivers: SQLite (WAL mode) for DAG nodes, sessions, out-of-band observation_blobs, and FTS5 cybernetic memories, plus JSONL, remote HTTP proxy, and AES-256-GCM vault encryption (ADR-009, ADR-014, ADR-021)."
tags:
  - please
  - storage
  - sqlite
  - memory
  - receipts
  - index
timestamp: "2026-10-11T10:23:00-07:00"
---

# Package internal/storage Index (`internal/storage`)

The `internal/storage` package (Layer 1: Infrastructure) isolates all persistent conversation, telemetry, and semantic memory drivers behind unified interfaces (`Storage`, `MemoryStore`, and `ObservationBlobStore`), per [ADR-009](../../decisions/009-modular-storage-extraction.md), [ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md), and [ADR-021](../../decisions/021-tiered-sensory-storage-and-observation-compaction.md).

## Core Files & Components

* [storage.go](storage.go) - Declares the `Storage` interface (DAG nodes, session heads, GC), `MemoryStore` interface (`Memory`, `MemoryScope`, `MemoryCategory`, `MemoryDiagnostics`, `SaveMemory`, `GetMemory`, `QueryMemories`, `DeleteMemory`, `PruneMemories`, `DiagnoseMemories`), and `ObservationBlobStore` interface (`SaveObservationBlob`, `GetObservationBlob`).
* [sqlite.go](sqlite.go) - Authoritative SQLite engine (`modernc.org/sqlite` in WAL mode) managing four primary tables:
  1. `nodes`: Conversation DAG nodes with non-destructive `ON CONFLICT` observation upserts.
  2. `sessions`: Named session head pointers (`main`, `sub_<id>`, etc.).
  3. `observation_blobs` (L2 Tier): Out-of-band gzipped telemetry storage keyed by content-addressable `obs_<hash>` IDs ([ADR-021](../../decisions/021-tiered-sensory-storage-and-observation-compaction.md)).
  4. `memories` + `memories_fts` (L3 Tier): Persistent cybernetic memory store with FTS5 full-text indexing, fallback `LIKE` / post-decryption search, confidence scoring, and access count tracking ([ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md)).
* [jsonl.go](jsonl.go) - Append-only JSON Lines flat-file driver for lightweight streaming logs and legacy compatibility.
* [remote.go](remote.go) - `RemoteDaemonStorage` proxy delegating node, session, and graph persistence over HTTP REST v1 to a running `please serve` daemon.
* [crypto.go](crypto.go) - AES-256-GCM authenticated encryption (`EncryptField`, `DecryptField`, `enc:v1:...` prefix) for zero-knowledge protection of node content, thoughts, observations, and memories at rest.
* [storage_test.go](storage_test.go), [blob_test.go](blob_test.go), [crypto_test.go](crypto_test.go), & [remote_test.go](remote_test.go) - Hermetic test suites verifying WAL concurrency, gzipped blob round-trips, encrypted FTS5 memory queries, ranking diagnostics, and remote daemon proxying.
