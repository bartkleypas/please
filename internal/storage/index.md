---
type: Concept
title: "Package internal/storage Index"
description: "Persistence storage engine drivers: SQLite (WAL mode), JSONL streaming logs, remote storage proxy, and AES-256 vault encryption (ADR-009)."
tags:
  - please
  - storage
  - sqlite
  - index
timestamp: "2026-09-10T11:42:00-07:00"
---

# Package internal/storage Index

The `internal/storage` package isolates all persistent conversation storage drivers behind a common interface (`Storage`), per [ADR-009](../../decisions/009-modular-storage-extraction.md).

## Core Files & Components

*   [storage.go](storage.go) - Declares the `Storage` interface, migration contracts, and common storage errors.
*   [sqlite.go](sqlite.go) - Authoritative SQLite engine utilizing Write-Ahead Logging (WAL) mode, parent indexing, and conflict-safe node observation updates.
*   [jsonl.go](jsonl.go) - Append-only JSON Lines flat-file driver for lightweight and streaming audit records.
*   [remote.go](remote.go) - Storage proxy delegating persistence operations over HTTP REST v1 to a remote `please serve` daemon.
*   [crypto.go](crypto.go) - AES-256 GCM encryption wrapper for zero-knowledge vault protection at rest.
*   [storage_test.go](storage_test.go) & [remote_test.go](remote_test.go) - Concurrency, stress, transaction, and migration test suites.
