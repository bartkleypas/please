---
type: Decision
title: "Architecture Decision Records (ADRs) Index"
description: "This index tracks key architectural decisions and engineering conventions throughout the lifecycle of the Please TUI application."
tags:
  - please
  - go
  - decision
  - index
timestamp: "2026-07-05T14:31:50-07:00"
---

# Architecture Decision Records (ADRs) Index

This index tracks key architectural decisions and engineering conventions throughout the lifecycle of the `Please` TUI application.

## Decision Records

*   [001-tui-framework](001-tui-framework.md) - Selection of Charm Bubble Tea and Lipgloss for the terminal user interface.
*   [002-graph-sqlite-storage](002-graph-sqlite-storage.md) - SQLite (WAL mode) database choice for persisting Directed Acyclic Graph (DAG) conversation nodes.
*   [003-embedded-visualizer](003-embedded-visualizer.md) - Embed a lightweight Go HTTP server with static HTML/JS visualizer assets.
