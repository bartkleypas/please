---
type: Concept
title: "Package internal/tools Index"
description: "Host tool registry, 3-tier sandbox policy enforcement, sensory/mutation/execution tools, SmartReceipt paging, cybernetic memory tools, and subagent delegation (ADR-005, ADR-011, ADR-014, ADR-021, ADR-022)."
tags:
  - please
  - tools
  - sandbox
  - memory
  - receipts
  - delegation
  - index
timestamp: "2026-10-11T10:23:00-07:00"
---

# Package internal/tools Index (`internal/tools`)

The `internal/tools` package (Layer 1: Infrastructure) provides host workspace interaction capabilities, JSON schema definitions, sandbox perimeter enforcement, observation receipt paging, persistent memory tools, and subagent delegation per [ADR-005](../../decisions/005-modular-tools-extraction.md), [ADR-011](../../decisions/011-agent-sandboxing-execution-isolation.md), [ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md), [ADR-021](../../decisions/021-tiered-sensory-storage-and-observation-compaction.md), and [ADR-022](../../decisions/022-native-delegated-multi-agent-topologies.md).

## Core Files & Components

* [registry.go](registry.go) - Central `Registry` managing tool registration (`RegisterDefaults`), deterministic KV-cache sequence ordering (`CategorySensory` $\rightarrow$ `CategoryMutate` $\rightarrow$ `CategoryExecute`), and 3-tier sandbox policy filtering (`strict`, `standard`, `permissive`).
* [sandbox.go](sandbox.go) - Security perimeter gatekeeper: workspace path confinement (`ValidateSafePath`), Git worktree path virtualization/rebasing, `SensitivePathPatterns` credential quarantine (`.env`, `.ssh`, `.aws`, `.please/vault.db`, `*.pem`), and argument extraction helpers.
* [sense.go](sense.go) - Workspace perception tools (`CategorySensory`):
  * `read_file`: Offset/limit line windowing, 64 KB byte budget, and non-punitive continuation coordinates.
  * `list_directory` & `list_files_recursive`: Safe directory traversal skipping `.git` and `.please`.
  * `grep_search`: Fast regex search across text files with automatic binary/SQLite file skipping (`isBinaryFile`).
* [mutate.go](mutate.go) - Workspace modification tools (`CategoryMutate`):
  * `write_file` (with explicit `overwrite` guard), `append_file` (with line-boundary hygiene), `edit_file` (`replace_string`, `insert_after`, `replace_regex` with multi-match ambiguity protection), and `delete_file`.
* [exec.go](exec.go) - Host compute tool (`CategoryExecute`):
  * `execute_command`: Gated shell execution with environment variable credential scrubbing (`sanitizeEnvironment`), binary allow-listing (`DefaultAllowedCommands`), and shell metacharacter validation.
* [receipt.go](receipt.go) - L2 Sensory Paging tool (`CategorySensory`, [ADR-021](../../decisions/021-tiered-sensory-storage-and-observation-compaction.md)):
  * `inspect_receipt`: Dereferences past `obs_<hash>` Smart Receipts from `observation_blobs` (or legacy node observations) with 1-indexed line slicing (`offset`, `limit`) and regex `grep` filtering.
* [memory.go](memory.go) - L3 Cybernetic Memory tool suite ([ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md)):
  * `memory_store` (`CategoryMutate`), `memory_recall` (`CategorySensory`), `memory_delete` (`CategoryMutate`, with fuzzy key suggestions on miss), and `memory_diagnose` (`CategorySensory`).
* [delegate.go](delegate.go) - Multi-Agent Delegation tools ([ADR-022](../../decisions/022-native-delegated-multi-agent-topologies.md)):
  * `spawn_subagent` (`CategoryExecute`): Spawns a bounded child session with isolated Git worktree sandboxing (`isolate_worktree`), tool preset (`read_only` vs `full` safety guard), and optional nested repository `target_dir`.
  * `inspect_subagent` (`CategorySensory`): Forensically inspects a child session's internal steps, modified files, and git diff stats.
  * `reconcile_subagent` (`CategoryMutate`): Merges (`merge` / `squash`) or discards (`discard`) a subagent's isolated worktree branch back into the parent checkout.
* [tools_test.go](tools_test.go), [sandbox_test.go](sandbox_test.go), [exec_test.go](exec_test.go), [registry_test.go](registry_test.go), [receipt_test.go](receipt_test.go), & [delegate_test.go](delegate_test.go) - Hermetic unit tests covering all tool contracts and security boundaries.
