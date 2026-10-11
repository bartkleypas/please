---
type: Concept
title: "Command Entry Point Index"
description: "The cmd/please package contains the main CLI entry point, subcommand routers, workspace initialization ceremony, ACP stdio server launcher, and vault/memory diagnostics."
tags:
  - please
  - go
  - cli
  - index
timestamp: "2026-10-11T10:22:00-07:00"
---

# Command Entry Point Index (`cmd/please`)

The `cmd/please` package (Layer 3: Presentation & Entrypoints) serves as the unified CLI binary for `Please`. It supports both standard subcommand dispatch (`please <subcommand>`) and multicall binary invocation (`please-acp` symlink for IDE registration).

## Core Source Files

* [main.go](main.go) - Primary entrypoint, CLI flag parsing, standalone TUI startup, `please serve` daemon launcher, `please connect` remote client setup, and `please cert generate` PKI minting.
* [init.go](init.go) - Implements `please init` (`-g/--global`, `-w/--workspace`, `-f/--force`) to scaffold self-contained `.please/` workspace or `~/.please/` global environments with SQLite WAL vaults and gitignore hygiene ([ADR-016](../../decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md)).
* [acp.go](acp.go) - Implements `please acp` (and `please-acp` multicall entrypoint), launching the Agent Client Protocol (ACP) JSON-RPC 2.0 server over `stdio` with strict stdout isolation ([ADR-012](../../decisions/012-agent-client-protocol-support.md)).
* [diagnostics.go](diagnostics.go) - Implements `please inspect` (DAG node metadata, `SmartReceipt` badges, and lineage shaper scorecards) and `please context` (exact reconstructed `[]domain.Message` prompt sequence with `--json` output).
* [memory.go](memory.go) - Implements `please memory` (`list`, `inspect <key>`, `diagnose`, `prune`) for inspecting and maintaining the L3 persistent SQLite cybernetic memory store ([ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md)).
* [vault.go](vault.go) - Implements `please vault` (`decrypt`, `clean`) for zero-knowledge SQLite vault decryption and orphaned observation blob cleanup.
* [acp_e2e_test.go](acp_e2e_test.go), [diagnostics_test.go](diagnostics_test.go), [init_test.go](init_test.go), [memory_test.go](memory_test.go), & [vault_test.go](vault_test.go) - End-to-end CLI and subcommand verification suites.

---

## Operational Modes & Subcommands

### 1. Standalone Interactive TUI & One-Shot CLI (`please`)
Launches the Charm Bubble Tea TUI wired directly to an in-process `SessionHarness` via `LocalHarnessProvider` ([ADR-019](../../decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md)). Also supports non-interactive CLI piping (`stdin`), positional message generation, node jumping (`-j`), and custom session heads (`--session`).

### 2. Workspace Initialization (`please init`)
Establishes a hermetic `.please/` directory (`config.json` + `vault.db`) at the workspace root or initializes the global `~/.please/` anchor (`please init --global`).

### 3. Headless Daemon (`please serve`)
Starts the multi-session HTTP/TLS engine daemon providing REST v1 endpoints, SSE turn streaming (`POST /api/v1/chat/stream`), per-session `SessionActor` mailboxes, and optional out-of-tree Git worktree isolation (`--worktree`).

### 4. Connected Remote TUI (`please connect [url]`)
Connects the interactive TUI to a running `please serve` daemon over HTTP/TLS using `RemoteDaemonProvider` and `RemoteDaemonStorage`.

### 5. IDE Agent Client Protocol (`please acp` / `please-acp`)
Runs the stdio JSON-RPC 2.0 ACP server for IDE integration (Zed, JetBrains, Xcode), complete with interactive tool permission gates and the Xcode preamble quarantine shim.

### 6. Diagnostics & Maintenance (`inspect`, `context`, `memory`, `vault`, `cert`)
* `please inspect [node-id] [-v <vault>] [-c <config>]` — Inspects node segments, tool calls, `SmartReceipt` telemetry, and topological path scorecards.
* `please context [node-id] [-v <vault>] [-c <config>] [--json]` — Reconstructs and prints the exact LLM prompt payload shaped by `ContextShaper`.
* `please memory [list|inspect|diagnose|prune]` — Queries and audits L3 semantic memories.
* `please vault [decrypt|clean]` — Decrypts encrypted vaults or purges unreferenced telemetry blobs.
* `please cert generate` — Mints 20-year self-signed ECDSA Root CA and Server Leaf certificates.
