---
type: Reference
title: "🦉 The Please Application"
description: "Please is a lightweight terminal user interface (TUI) application designed for fast, dynamic interaction with Large Language Models (LLMs)."
tags:
  - please
  - go
timestamp: "2026-05-21T12:10:44-07:00"
---

# 🦉 The Please Application

`Please` is a lightweight terminal user interface (TUI) application designed for fast, dynamic interaction with Large Language Models (LLMs).

Unlike linear chat applications, `Please` treats conversations as a Directed Acyclic Graph (DAG), allowing you to branch reality, jump between timelines, and maintain a persistent history. The application features a modular TUI architecture, real-time response streaming, and a robust navigation suite for complex narrative exploration.

---

## ✨ Features

### 🏗️ Engine Architecture
*   **DAG-Based Navigation:** Breaks the linear chat mold by allowing users to branch, prune, and jump across multiple conversation timelines.
*   **Persistent Storage:** Narrative history is stored in a robust SQLite database (default) or legacy JSONL format, ensuring your stories are saved safely and concurrently.
*   **Context Resonance Scoring:** Automatically manages the LLM context window using an exponential decay algorithm (`V = (W * C) * e^(-k * Δt)`). It dynamically prunes raw tool outputs and internal reasoning from older messages while preserving the high-fidelity semantic core of the dialogue, preventing context bloat during heavy tool usage.
*   **Streaming Responses:** Real-time, character-by-character output for a responsive and interactive experience.

### 🎭 Narrative Interaction
*   **Modular TUI:** Built with [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea), featuring a flattened state machine and specialized event handlers.
*   **Visual Mapping:** Use `/map` to visualize the branching paths of your story with Vim-style navigation (`h/j/k/l`).
*   **Supernodes (Compaction):** Press `c` in the map to summarize long-winded branches into high-density context nodes, helping to prevent the onset of "context bloat" in longer conversations (especially useful after heavy tool use).
*   **Branch Pruning:** Press `d` in the map to soft-delete unwanted branches, with `/gc` for permanent secure scrubbing.
*   **Engine Daemon & Streaming API:** Run `please serve --port 8080` to launch the headless API service. Features a Server-Sent Events (SSE) streaming endpoint (`POST /api/v1/chat/stream`), complete DAG REST API (`/api/v1/graph`, `/api/v1/nodes`), and host tool execution loops for client applications (Swift macOS/iPadOS, Web UI, CLI).
*   **20-Year Internal PKI Certificates:** Run `please cert generate` to mint 20-year self-signed Root CA and Server Leaf certificates with SANs for zero-expiration TLS across local and home mesh networks.
*   **Persona Management:** Easily switch between different system prompts and characters using the `/persona` command.
*   **Natural Reading Pacing:** Buffer and stream assistant replies at a realistic human reading pace with punctuation-sensitive pauses (300ms for ends of sentences, 100ms for commas). Supports instant skipping to flush the output.

---

## 🔗 CLI Interaction & Chaining

`Please` can be used programmatically to build narratives or interact with the graph directly from the terminal.

### Add a message via Stdin (Piping)
```bash
echo "As the fog cleared, a mysterious figure appeared." | please
```
*Infers role as `tool`, generates a response, and outputs the assistant's Node ID.*

### Add a message via Positional Arguments
```bash
please "What is the capital of France?"
```
*Infers role as `user`, generates a response, and outputs the assistant's Node ID.*

### Chain messages
```bash
ROOT_ID=$(please --role system "You are a detective in 1920s London.")
MSG_ID=$(please --parent $ROOT_ID "I want to investigate the docks.")
please -c --jump $MSG_ID
```

### One-shot Insertion (No Gen)
```bash
please --no-gen "This message is just for the graph history."
```

---

## 🚀 Getting Started

### Prerequisites
*   **Go:** 1.26 or higher.
*   **LLM Provider:**
    *   A local **Ollama** instance running with your preferred model (default: `gemma4:e4b`).
    *   *OR* an **OpenAI** API key and compatible endpoint.

### Installation

**Option A: Install via Go (Recommended)**
If you have Go installed, you can install the binary directly:
```bash
go install github.com/bartkleypas/please@latest
```

**Option B: Build from Source**
1.  Clone the repository:
    ```bash
    git clone https://github.com/bartkleypas/please.git
    cd please
    ```
2.  Install dependencies:
    ```bash
    go mod download
    ```

### Running the App
The project includes a `Makefile` to simplify building and versioning (injecting git tags via `ldflags`).

Build and verify the application:
```bash
make build
./please --version
```

Build and launch the TUI chat interface instantly:
```bash
make run
```

Start the headless API and streaming engine daemon:
```bash
./please serve --port 8080
```

Connect the TUI to a remote Please daemon:
```bash
./please connect http://localhost:8080
```

Generate 20-year internal Root CA and Server certificates for TLS:
```bash
./please cert generate
./please serve --port 8443 --tls --generate-certs
```

Or start the TUI with the embedded web visualizer directly:
```bash
./please -c -s 8080
```

### Configuration
`Please` reads from `~/.config/please/config.json` (or `~/Library/Application Support/please/config.json` on macOS). You can configure the LLM provider, inference parameters, workspace root, and encryption settings here:

**Local Ollama (Default)**
```json
{
  "provider": "ollama",
  "endpoint": "http://localhost:11434/api/chat",
  "model": "gemma4:e4b",
  "workspace_dir": "~/Code",
  "natural_pacing": true,
  "options": {
    "temperature": 0.7,
    "top_p": 0.9,
    "top_k": 40,
    "num_ctx": 16384,
    "max_tokens": 4096
  }
}
```

**OpenAI API / Local OpenAI-Compatible Server**
```json
{
  "provider": "openai",
  "endpoint": "https://api.openai.com/v1/chat/completions",
  "model": "gpt-4o",
  "api_key": "sk-your-api-key",
  "workspace_dir": "$HOME/Code/my-project",
  "encryption_key": "your-secret-encryption-key",
  "options": {
    "temperature": 0.7,
    "top_p": 0.9,
    "max_tokens": 4096
  }
}
```

*Note: `workspace_dir` supports `~` expansion, `$HOME` environment variable expansion, relative paths, and trailing slashes.*

---

## 🤖 Runtime Commands

Inside the app, use these interactive commands to navigate your story:

| Command | Description |
| :--- | :--- |
| `/help` | View the command list and navigation tips. |
| `/map` | Display the visual tree of the DAG. |
| `/list` | List all node IDs in the narrative graph. |
| `/jump <id>` | Teleport to a specific node using its ID prefix. |
| `/mark [id]` | Place a bookmark at the current or specified node. |
| `/unmark <id>` | Remove a bookmark from a node. |
| `/persona` | Branch the story into a new timeline with a new system prompt. |
| `/config` | View current configuration sheet (with redacted encryption secrets). |
| `/config workspace <path\|default>` | Set or reset the active workspace directory root. |
| `/config key <secret\|default>` | Set or clear vault encryption key. |
| `/config temp <val\|default>` | Adjust sampling temperature (e.g. `0.7`). |
| `/config top_p <val\|default>` | Adjust top-p nucleus sampling (e.g. `0.9`). |
| `/config top_k <val\|default>` | Adjust top-k sampling (e.g. `40`). |
| `/config ctx <val\|default>` | Adjust context window size in tokens (e.g. `16384`). |
| `/config max_tokens <val\|default>` | Adjust maximum response tokens (e.g. `4096`). |
| `/server` | Control the web visualization server (`/server on`, `/server off`). |
| `/version`| Display the application version and git commit hash. |
| `/gc` | Garbage collect deleted nodes from the database. |
| `/audit` | Toggle audit mode to view extended UUIDs for each node. |
| `/pacing` | Toggle natural reading pacing for LLM stream (`/pacing on`, `/pacing off`). |
| `/q` or `/bye` | Gracefully exit the application. |

**Navigation Tips:**
*   **Chat:** Use `↑`/`↓` or `PgUp`/`PgDn` to scroll history.
*   **Streaming Pacing:** Press `ESC`, `Enter`, or `Space` while content is streaming to instantly skip pacing and flush the whole response.
*   **Map Navigation:**
    *   `j`/`k`: Move selection.
    *   `h`/`l`: Collapse/Expand or Ascend/Descend branches.
    *   `g`/`G`: Jump to root/end.
    *   `/`: Real-time fuzzy search.
    *   `c`: Compact/Summarize branch.
    *   `d`: Prune/Delete branch.
*   **Exit Views:** Press `ESC` to return to the chat from `/map` or `/help` views.

---

## 🧪 Development & Testing

`Please` is built with testability in mind, utilizing mock LLM providers and state-machine testing.

### Running Tests
Run the fast, offline unit test suite:
```bash
make test
```

Run the full end-to-end "Live Fire" integration tests (requires a running Ollama instance):
```bash
make test-livefire
```

### Technical Structure
*   `cmd/please/`: Entry point and CLI flag handling.
*   `internal/engine/`: Core logic for DAG management, LLM providers, and storage.
*   `internal/tui/`: Bubble Tea components, modular handlers, and rendering logic.
