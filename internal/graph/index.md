---
type: Concept
title: "Package internal/graph Index"
description: "Pure in-memory Directed Acyclic Graph (DAG) data structure, Node models, topological sorting, and cycle detection (ADR-010)."
tags:
  - please
  - graph
  - dag
  - index
timestamp: "2026-09-10T11:42:00-07:00"
---

# Package internal/graph Index

The `internal/graph` package defines the pure, memory-only conversation DAG data structures decoupled from database persistence and LLM execution logic, as established in [ADR-010](../../decisions/010-pure-dag-graph-extraction.md).

## Core Files & Components

*   [node.go](node.go) - Defines `Node`, `Role` (system, user, assistant, tool), timestamps, token metrics, observations, tool calls, and parent pointers.
*   [graph.go](graph.go) - Implements `Graph`, thread-safe node indexing, parent trail reconstruction, branch discovery, leaf identification, and adversarial cycle detection.
*   [graph_test.go](graph_test.go) - Unit tests testing graph invariants, deep branching, parent walking, and cycle detection assertions.
