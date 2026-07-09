---
type: Concept
title: "Change Log"
description: "All updates and modifications to this knowledge bundle are tracked chronologically below."
tags:
  - please
  - go
  - log
timestamp: "2026-07-08T11:02:03-07:00"
---

# Change Log

All updates and modifications to this knowledge bundle are tracked chronologically below.

## 2026-07-05

*   **Initialized OKF Bundle**: Created the project-level [index.md](index.md) and [log.md](log.md).
*   **Structured Decisions Index**: Created the [decisions/index.md](decisions/index.md) sub-index and initialized ADRs [001-tui-framework.md](decisions/001-tui-framework.md) (Bubble Tea/Lipgloss), [002-graph-sqlite-storage.md](decisions/002-graph-sqlite-storage.md) (SQLite WAL mode), and [003-embedded-visualizer.md](decisions/003-embedded-visualizer.md) (Embedded D3 server).
*   **Documented Core Concepts**: Added [context_resonance.md](context_resonance.md) describing the exponential decay pruning algorithm, and [natural_pacing.md](natural_pacing.md) detailing the punctuation-sensitive stream streaming loop.
*   **Documented Engine Package Structure**: Created the sub-index at [internal/engine/index.md](internal/engine/index.md) describing the core Go packages for the DAG graph model, database persistence, LLM client drivers, and JIT tools.
*   **Documented TUI Package Structure**: Created the sub-index at [internal/tui/index.md](internal/tui/index.md) mapping Bubble Tea model states, TUI event handlers, visual view modules, keymaps, and streaming components.

## 2026-07-06

*   **Refactored Automated Context Supplement**: Adjusted `generateSystemSupplement()` in [internal/tui/handlers.go](internal/tui/handlers.go) to read from `index.md` instead of `README.md` for folder-level index knowledge.

## 2026-07-08

*   **Fixed CLI and TUI Harness Bugs**:
    *   Resolved validation crash on CLI piped stdin by auto-generating tool IDs for `RoleTool` node creation in [service.go](internal/engine/service.go).
    *   Enabled combining piped stdin context and positional arguments sequentially in [main.go](cmd/please/main.go).
    *   Fixed TUI `ctrl+c` exit failure in `/map` mode by hoisting the exit check to the top of `handleKeyEvent` in [keys.go](internal/tui/keys.go).
    *   Stopped hidden text input keystroke capture when viewport overrides are active in [keys.go](internal/tui/keys.go).

## 2026-07-09

*   **Approved ssh Tool**: Added `ssh` to the `allowedCommands` list in [tool_defaults.go](file:///Users/Bart/Code/please/internal/engine/tool_defaults.go) to allow the harness engine to use SSH commands.

