---
type: Concept
title: "Command Entry Point Index"
description: "The cmd/please package contains the main entry point for the application, handling subcommand routing and execution modes."
tags:
  - please
  - go
  - cli
timestamp: "2026-08-21T15:00:00-07:00"
---

# Command Entry Point Index

The `cmd/please` package contains the `main.go` file, which serves as the primary entry point for the `Please` application. It provides a CLI-based dispatcher for different operational modes.

## Execution Modes

### 1. Standalone Interactive (Default)
The default mode launches the **Terminal User Interface (TUI)**. It initializes a local session by:
*   Loading local configurations and workspace settings.
*   Initializing local storage (SQLite or JSONL).
*   Setting up a local LLM provider.
*   Launching the Bubble Tea event loop.
*   **Capabilities**: Supports piping content via `stdin` and passing messages as command-line arguments.

### 2. Daemon Mode (`please serve`)
Starts a headless background service that acts as the authoritative engine for the graph.
*   **Capabilities**: Provides a REST v1 API and an SSE (Server-Sent Events) stream for real-time dialogue.
*   **Security**: Supports TLS encryption via a self-contained, 20-year internal PKI.
*   **Observability**: Can be configured to host a web-based visualization of the conversation graph.

### 3. Remote Client Mode (`please connect`)
Allows a TUI instance to act as a remote client, connecting to an existing `please serve` daemon.
*   **Capabilities**: Synchronizes the conversational graph and message history over a network connection using the `RemoteDaemon` protocol.
*   **Security**: Supports Bearer token authentication and TLS certificate verification.

## Utility Commands

### Certificate Generation (`please cert generate`)
A specialized utility to generate the long-term (20-year) ECDSA Root CA and Leaf Server Certificates required for secure local network communication.
