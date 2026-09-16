---
type: Concept
title: "Package internal/tools Index"
description: "Tool registry, sandbox constraints, filesystem manipulation, search utilities, and command execution (ADR-005)."
tags:
  - please
  - tools
  - sandbox
  - index
timestamp: "2026-09-10T11:42:00-07:00"
---

# Package internal/tools Index

The `internal/tools` package provides safe host workspace interaction tools, schema generation, argument validation, and directory sandboxing per [ADR-005](../../decisions/005-modular-tools-extraction.md).

## Core Files & Components

*   [registry.go](registry.go) - Central registry managing tool capabilities, built-in tool registration (`RegisterDefaults`), deterministic KV-cache sequence ordering, and three-tiered policy filtering (`strict`, `standard`, `permissive`).
*   [sandbox.go](sandbox.go) - Perimeter gatekeeper: workspace boundaries (`ValidateSafePath`), `SensitivePathPatterns` credential quarantine, path canonicalization, and tool argument helpers.
*   [sense.go](sense.go) - Perception tools (`CategorySensory`): `read_file` (with byte windowing and pagination), `list_directory`, `list_files_recursive`, and `grep_search` using unified safe tree traversal.
*   [mutate.go](mutate.go) - State modification tools (`CategoryMutate`): `write_file`, `append_file`, `edit_file` (targeted surgical edits), and `delete_file` bounded by workspace sandboxing.
*   [exec.go](exec.go) - Host compute tools (`CategoryExecute`): `execute_command` with environment sanitization (`sanitizeEnvironment`), compile-time allow-lists (`DefaultAllowedCommands`), and pipeline injection validation.
*   [tools_test.go](tools_test.go), [sandbox_test.go](sandbox_test.go), & [exec_test.go](exec_test.go) - Hermetic test suites verifying sensory pagination, mutations, path quarantine, and shell execution isolation.
