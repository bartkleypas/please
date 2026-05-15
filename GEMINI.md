# GEMINI.md

## Project Overview
`Please` is a lightweight Terminal User Interface (TUI) application designed for dynamic interaction with Large Language Models (LLMs). Its core innovation is managing conversations as a **Directed Acyclic Graph (DAG)** rather than a linear history, allowing users to branch narratives, switch contexts (personas), and maintain persistent history across sessions.

## Core Technologies
- **Language:** Go (1.26+)
- **TUI Framework:** [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea)
- **Styling:** [Charm Lipgloss](https://github.com/charmbracelet/lipgloss)
- **LLM Integration:** Local [Ollama](https://ollama.com/) (defaulting to `gemma4:e4b`) and [OpenAI API](https://openai.com/) natively supported.
- **ID Generation:** [Google UUID](https://github.com/google/uuid)

## Architecture & Key Components
The project is split into two primary internal modules:

### `internal/engine` (Core Logic)
- **`Graph` (`graph.go`)**: Manages the DAG of `Node` objects. Each node represents a message. Supports real-time re-parenting for branch compaction and cycle detection.
- **`Manager` (`service.go`)**: The central coordinator. Implements `CompactRange` for generating high-density Supernodes, `PruneBranch` for soft-deleting sub-trees, and **Adversarial Validation** to ensure structural integrity.
- **`LLMProvider` (`llm.go`)**: Interface for LLM backends. Handles standard, streaming, and specialized summarization calls. Features hybrid tool parsing (Native Ollama + Manual `<tool_call>` tags).
- **`Storage` (`storage.go`)**: SQLite (WAL mode) persistence. Supports `deleted` flags and secure disk scrubbing via `VACUUM`.
- **`Tool` (`tool.go`)**: Framework for LLM tool calling.

### `internal/tui` (User Interface)
- **`Model` (`model.go`)**: Main Bubble Tea state machine.
- **`View` (`view.go`)**: Renders chat, `/map` visualization, and context-aware footers.
- **`Handlers` (`handlers.go`)**: Manages event logic, including Vim-style navigation (`h/j/k/l`), fuzzy search (`/`), and branch compaction (`c`).
- **`Commands` (`commands.go`)**: Implementation of slash commands (e.g., `/jump`, `/persona`, `/gc`).

## Key Features & Conventions

### DAG Compaction (Supernodes)
Users can compress thematic clusters into a single `summary` node by pressing `c` in the map view. This "grafts" the active branch onto a new Supernode, preserving LLM context while decluttering the graph.

### Friction-Free CLI
The application supports direct interaction via positional arguments and pipes:
```bash
please "What is the capital of France?"  # User role inference
cat README.md | please "Summarize this" # Tool/Context role inference
```
- **The Silicon Seed:** Piping a document with `--role system` (and no parent) automatically births a new DAG Root, enabling isolated agent bootstrapping.

### Navigation & Management
- **Vim-style navigation:** `h` (collapse/ascend), `l` (unfold/descend), `j/k` (move).
- **Audit Mode:** Toggle with `v` or `/audit` to reveal internal reasoning nodes and full UUIDs.
- **Pruning:** `d` in map view soft-deletes a branch; `/gc` permanently scrubs the database.
- **Fuzzy Search:** `/` in map view filters nodes in real-time.

### `internal/server` (Web Visualization)
- **`Server` (`server.go`)**: Embedded HTTP server providing a threaded web view of the conversation graph. (Updates require page refresh).
- **`Assets` (`assets/index.html`)**: HTML/JS frontend for the web visualization, using "bubble-up" activity sorting.

## Building, Running, and Testing

### Build
```bash
go build -o please ./cmd/please
```

### Run
```bash
# Run the application in chat mode
go run cmd/please/main.go -c

# Run with the web visualization server enabled
go run cmd/please/main.go -c -s
```

### Test
```bash
# Run all tests
go test ./...
```

## Configuration and Storage
The application automatically manages its configuration and data directories:
- **Configuration:** `~/.config/please/config.json` (Customizable provider, api_key, model, and endpoint).
- **Storage (Vault):** `~/.local/share/please/vault.db` (SQLite database containing all conversation nodes).

## Development Conventions
- **DAG Navigation:** Always consider that a "current" state in the app is a specific leaf node in the DAG. History is reconstructed by traversing up to the root.
- **Streaming:** LLM responses are streamed character-by-character via Go channels into the Bubble Tea event loop.
- **Mocking:** Use `internal/engine/mock.go` for testing TUI and engine logic without a live Ollama instance.
- **Surgical Edits:** When modifying the TUI, ensure that new messages or commands are integrated into the `Update` loop in `internal/tui/tui.go`.
