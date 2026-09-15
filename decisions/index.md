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
*   [004-swift-apple-ecosystem-evolution](004-swift-apple-ecosystem-evolution.md) - Swift port strategy, iPad Mini device jump, and Intel Mac development bridge strategy.
*   [005-modular-tools-extraction](005-modular-tools-extraction.md) - Extraction and decomposition of tool execution, sandboxing, and default tools from internal/engine into internal/tools.
*   [006-modular-providers-extraction](006-modular-providers-extraction.md) - Extraction of LLM provider drivers, wire serialization protocols, and message contracts from internal/engine into internal/providers.
*   [007-multi-session-daemon-branch-concurrency](007-multi-session-daemon-branch-concurrency.md) - Multi-session daemon architecture, cursor decoupling, branch-isolated compactions, and workspace sandboxing.
*   [008-modular-config-extraction](008-modular-config-extraction.md) - Extraction of configuration schemas, directory discovery, and migration into internal/config.
*   [009-modular-storage-extraction](009-modular-storage-extraction.md) - Extraction of SQLite WAL, JSONL, remote storage proxy, and vault encryption into internal/storage.
*   [010-pure-dag-graph-extraction](010-pure-dag-graph-extraction.md) - Extraction of the pure in-memory conversation Node and DAG Graph data structures into internal/graph.
*   [011-agent-sandboxing-execution-isolation](011-agent-sandboxing-execution-isolation.md) - Hardened agent sandboxing, credential quarantine, policy tiering (demoting raw shell access), and execution isolation without external container dependencies.
*   [012-agent-client-protocol-support](012-agent-client-protocol-support.md) - Agent Client Protocol (ACP) support, stdio JSON-RPC harness integration, and human-in-the-loop tool authorization for modern IDEs (Zed, JetBrains, Xcode).

