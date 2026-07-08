---
type: Concept
title: "Package internal/engine Index"
description: "The internal/engine package contains the core logic of the Please application. It coordinates conversational memory graphs, database storage, LLM A..."
tags:
  - please
  - go
  - index
timestamp: "2026-07-05T14:44:42-07:00"
---

# Package internal/engine Index

The `internal/engine` package contains the core logic of the `Please` application. It coordinates conversational memory graphs, database storage, LLM APIs, and command execution.

## Core Files & Components

### 1. Conversational Graph Model
*   [node.go](node.go) - Defines the `Node` struct representing a single message, role, timestamp, token sizes, and parent links.
*   [graph.go](graph.go) - Implements the `Graph` struct to manage node relationships, build branch trails, and perform cycle detection.

### 2. Coordination & Compaction (The Service layer)
*   [service.go](service.go) - Contains `Manager` which coordinates TUI interactions. Implements:
    *   `CompactRange`: Summarizes historical nodes to create Supernodes.
    *   `PruneBranch`: Soft-deletes sub-trees.
    *   **Adversarial Validation**: Ensures the DAG structure remains consistent and free of cycles.

### 3. Database Persistence
*   [storage.go](storage.go) - Connects to the local SQLite database (`vault.db`), manages schemas, indexes parent paths, and executes structural queries.

### 4. LLM Providers
*   [llm.go](llm.go) - Interfaces for LLM endpoints and the default local Ollama client driver.
*   [openai.go](openai.go) - Client integration for the OpenAI API.

### 5. Function Calling & Tools
*   [tool.go](tool.go) - Declares tool definition schemas.
*   [tool_defaults.go](tool_defaults.go) - Defines standard system tools (such as terminal running or git diffing).

### 6. Configurations & Security
*   [config.go](config.go) - Parses local configurations (`~/.config/please/config.json`).
*   [crypto.go](crypto.go) - Handles AES-256 payload encryption to protect database conversations on disk.
