# GEMINI.md

## Project Overview
`Please` is a lightweight Terminal User Interface (TUI) application designed for dynamic interaction with Large Language Models (LLMs). Its core innovation is managing conversations as a **Directed Acyclic Graph (DAG)** rather than a linear history, allowing users to branch narratives, switch contexts (personas), and maintain persistent history across sessions.

## Core Technologies
- **Language:** Go (1.26+)
- **TUI Framework:** [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea)
- **Styling:** [Charm Lipgloss](https://github.com/charmbracelet/lipgloss)
- **LLM Integration:** Local [Ollama](https://ollama.com/) (defaulting to `gemma4:e4b`)
- **ID Generation:** [Google UUID](https://github.com/google/uuid)

## Architecture & Key Components
The project is split into two primary internal modules:

### `internal/engine` (Core Logic)
- **`Graph` (`graph.go`)**: Manages the DAG of `Node` objects. Each node represents a message in the conversation.
- **`Manager` (`service.go`)**: The central coordinator that links the graph, storage, and tool registry.
- **`LLMProvider` (`llm.go`)**: Interface for LLM backends. Currently implements `OllamaProvider` with support for both standard and streaming responses.
- **`Storage` (`storage.go`)**: Persistence layer supporting both SQLite (with WAL mode) and legacy JSONL formats.
- **`Tool` (`tool.go`)**: Framework for LLM tool calling (function calling).

### `internal/tui` (User Interface)
- **`Model` (`model.go`)**: The main Bubble Tea state machine.
- **`View` (`view.go`)**: Rendering logic for the chat interface and the `/map` DAG visualization.
- **`Commands` (`commands.go`)**: Implementation of slash commands (e.g., `/jump`, `/persona`, `/mark`).
- **`Messages` (`messages.go`)**: Definitions for Bubble Tea messages used for internal communication and streaming.

## Building, Running, and Testing

### Build
```bash
go build -o please ./cmd/please
```

### Run
```bash
# Run the application in chat mode
go run cmd/please/main.go -c
```

### Test
```bash
# Run all tests
go test ./...
```

## Configuration and Storage
The application automatically manages its configuration and data directories:
- **Configuration:** `~/.config/please/config.json` (Customizable model and endpoint).
- **Storage (Vault):** `~/.local/share/please/vault.db` (SQLite database containing all conversation nodes).

## Development Conventions
- **DAG Navigation:** Always consider that a "current" state in the app is a specific leaf node in the DAG. History is reconstructed by traversing up to the root.
- **Streaming:** LLM responses are streamed character-by-character via Go channels into the Bubble Tea event loop.
- **Mocking:** Use `internal/engine/mock.go` for testing TUI and engine logic without a live Ollama instance.
- **Surgical Edits:** When modifying the TUI, ensure that new messages or commands are integrated into the `Update` loop in `internal/tui/tui.go`.
