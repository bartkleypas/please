---
type: Decision
title: Embedded HTTP Web Visualizer Server
description: Decision to host an embedded HTTP server serving a static, vanilla HTML/JS D3 visualizer to render the conversation DAG, preserving a zero-dependency binary.
tags:
- adr
- architecture
- visualizer
- server
- go
timestamp: '2026-07-05T14:31:00-07:00'
---

# ADR 003: Embedded HTTP Web Visualizer Server

## Status
🏁 Approved

## Context
While the terminal map is useful for quick jumps, visualizing large conversation DAG trees is structurally limited in character grid UI consoles. A full graphical visualization is needed. However, introducing heavy modern web build pipelines (Node, npm, Vite) would break the Go project's portability and zero-dependency compilation goals.

---

## Decision
1.  **Embedded Go HTTP Server**: Bundle a lightweight HTTP server (`net/http`) inside the Go binary. It spins up in a background goroutine only if launched with the `-s` / `--server` CLI flag.
2.  **Static Visualizer Assets**: Serve a static, vanilla HTML/JS web frontpage (located at `internal/server/assets/index.html`) using Go's `embed` package. 
3.  **JSON API**: Expose simple read-only API endpoints (e.g. `/api/graph`) to fetch the node map from SQLite, feeding it into D3.js on the client web browser to render the radial tidy tree graph.
4.  **No Dynamic Web Compile**: Keep visualizer assets free of heavy JavaScript node compilation steps. The user does not need `node` installed to build or run the visualization.
