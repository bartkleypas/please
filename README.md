# 🦉 The Please Application

`Please` is a lightweight terminal user interface (TUI) application designed for fast, dynamic interaction with Large Language Models (LLMs).

Unlike linear chat applications, `Please` treats conversations as a Directed Acyclic Graph (DAG), allowing you to branch reality, jump between timelines, and maintain a persistent history. The application features a modular TUI architecture, real-time response streaming, and a robust navigation suite for complex narrative exploration.

---

## ✨ Features

### 🏗️ Engine Architecture
*   **DAG-Based Navigation:** Breaks the linear chat mold by allowing users to branch, prune, and jump across multiple conversation timelines.
*   **Persistent Storage:** Narrative history is stored in a robust SQLite database (default) or legacy JSONL format, ensuring your stories are saved safely and concurrently.
*   **Streaming Responses:** Real-time, character-by-character output for a responsive and interactive experience.

### 🎭 Narrative Interaction
*   **Modular TUI:** Built with [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea), featuring a flattened state machine and specialized event handlers.
*   **Visual Mapping:** Use `/map` to visualize the branching paths of your story with Vim-style navigation (`h/j/k/l`).
*   **Supernodes (Compaction):** Press `c` in the map to summarize long-winded branches into high-density context nodes, helping to prevent the onset of "context bloat" in longer conversations (especially useful after heavy tool use).
*   **Branch Pruning:** Press `d` in the map to soft-delete unwanted branches, with `/gc` for permanent secure scrubbing.
*   **Web Visualization:** Run with `-s` to start an embedded web server with a "Deep Forest Green" theme (but not *that* deep). Updates to the graph require refreshing the page to see changes.
*   **Persona Management:** Easily switch between different system prompts and characters using the `/persona` command.

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
*   **Ollama:** A local Ollama instance running with your preferred model (default: `gemma4:e4b`).

### Installation
1.  Clone the repository:
    ```bash
    git clone https://github.com/your-username/please.git
    cd please
    ```
2.  Install dependencies:
    ```bash
    go mod download
    ```

### Running the App
Launch the TUI chat interface:
```bash
go run cmd/please/main.go -c
```
Or start with the web visualization server:
```bash
go run cmd/please/main.go -c -s 8080
```
Or build and run:
```bash
go build -o please ./cmd/please
./please -c
```

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
| `/server` | Control the web visualization server (`/server on`, `/server off`). |
| `/gc` | Garbage collect deleted nodes from the database. |
| `/audit` | Toggle audit mode to view extended UUIDs (and more) for each node. |
| `/q` or `/bye` | Gracefully exit the application. |

**Navigation Tips:**
*   **Chat:** Use `↑`/`↓` or `PgUp`/`PgDn` to scroll history.
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
Run the full test suite (including engine and TUI logic):
```bash
go test ./...
```

### Technical Structure
*   `cmd/please/`: Entry point and CLI flag handling.
*   `internal/engine/`: Core logic for DAG management, LLM providers, and storage.
*   `internal/tui/`: Bubble Tea components, modular handlers, and rendering logic.
