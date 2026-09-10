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

*   [registry.go](registry.go) - Central registry managing available tools and generating OpenAI/Ollama function calling JSON schemas.
*   [sandbox.go](sandbox.go) - Sandboxing boundaries (`ValidateSafePath`), path canonicalization (`canonicalizePath` evaluating symlinks), and path virtualization (`primaryWorkspace` rebasing).
*   [fs.go](fs.go) - Filesystem tools: `read_file` (with 64KB budget and pagination headers), `write_file`, and `edit_file` (atomic regex/chunk replacement).
*   [exec.go](exec.go) - Shell command execution tool (`run_command`) with timeout handling and output buffering.
*   [search.go](search.go) - Directory search tools: `search_web`, `list_directory`, and file pattern matching.
*   [defaults.go](defaults.go) - Default registration factory populating tools with active sandbox constraints.
*   [tools_test.go](tools_test.go) & [sandbox_test.go](sandbox_test.go) - Test suites verifying boundary escape prevention, path virtualization, and file edits.
