---
type: Concept
title: "Package internal/graph Index"
description: "Pure in-memory Directed Acyclic Graph (DAG) data structure, Node models, topological sorting, and cycle detection (ADR-010)."
tags:
  - please
  - graph
  - dag
  - index
timestamp: "2026-10-11T10:22:00-07:00"
---

# Package internal/graph Index (`internal/graph`)

The `internal/graph` package (Layer 1: Infrastructure) defines the thread-safe, in-memory conversation Directed Acyclic Graph (DAG) data structures decoupled from database persistence and LLM execution logic, as established in [ADR-010](../../decisions/010-pure-dag-graph-extraction.md) and stratified in [ADR-018](../../decisions/018-package-stratification-and-domain-decoupling.md). It imports only `internal/domain` (Layer 0) and the Go standard library.

## Core Files & Components

* [node.go](node.go) - Defines `Node` (ID, `ParentID`, `Role`, `Content`, `Thought`, `Signat`, `Images`, `ToolCalls`, `Observations`, `Metadata`, `Deleted`, `Compacted`, `ResonanceWeight`, `Timestamp`) and role constants re-exported from `internal/domain`.
* [graph.go](graph.go) - Implements `Graph`, thread-safe (`sync.RWMutex`) node indexing, lineage path reconstruction (`GetPath`), child/branch discovery (`GetChildren`), short-ID prefix lookup (`FindNodeByShortID`), and adversarial cycle detection.
* [graph_test.go](graph_test.go) & [stress_test.go](stress_test.go) - Unit and concurrent stress tests verifying DAG invariants, deep branching, parent walking, and cycle rejection.
