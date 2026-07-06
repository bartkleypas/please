---
type: Decision
title: SQLite WAL-Mode Storage Engine for DAG Persistence
description: Decision to use SQLite running in Write-Ahead Logging (WAL) mode as the local relational storage engine to persist Directed Acyclic Graph (DAG) conversations.
tags:
- adr
- architecture
- sqlite
- database
- persistence
timestamp: '2026-07-05T14:31:00-07:00'
---

# ADR 002: SQLite WAL-Mode Storage Engine for DAG Persistence

## Status
🏁 Approved

## Context
Conversational message records, metadata, tool calling logs, and system branches require persistent, transactional storage. Relational databases are optimal for parent-pointer links, and local-first execution requires a zero-service runtime that doesn't need Docker or a separate daemon running on the user's workstation.

---

## Decision
1.  **SQLite Database**: Persist all graph data in a local SQLite file (default location: `~/.local/share/please/vault.db`).
2.  **WAL Mode Concurrency**: Configure the database connection with Write-Ahead Logging (WAL) mode enabled. This allows concurrent readers to query the database (e.g. the web visualizer backend) even while the TUI chat loop is writing new message stream blocks.
3.  **Relational DAG Schema**: Use standard relational tables:
    *   `nodes`: Stores message roles, text contents, UUIDv7 surrogate keys, and a nullable `parent_id` referencing back to the parent node.
    *   Indices on `parent_id` and timestamps to speed up backwards-traversal context generation.
