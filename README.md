# 🦉 The Please Application

`Please` is a lightweight terminal user interface (TUI) application designed for fast, dynamic interaction with Large Language Models (LLMs).

Unlike linear chat applications, `Please` treats conversations as a Directed Acyclic Graph (DAG), allowing you to branch reality, jump between timelines, and maintain a persistent history. The application features a modular TUI architecture, real-time response streaming, and a robust navigation suite for complex narrative exploration.

---

## ✨ Features

### 🏗️ Engine Architecture
*   **DAG-Based Navigation:** Breaks the linear chat mold by allowing users to branch, prune, and jump across multiple conversation timelines.
*   **Persistent Storage:** Narrative history is stored in a local JSONL format, ensuring your stories are saved across sessions.
*   **Streaming Responses:** Real-time, character-by-character output for a responsive and interactive experience.

### 🎭 Narrative Interaction
*   **Modular TUI:** Built with [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea), featuring a flattened state machine and specialized event handlers.
*   **Visual Mapping:** Use `/map` to visualize the branching paths of your story in a tree-like view.
*   **Persona Management:** Easily switch between different system prompts and characters using the `/persona` command.

---

## 🔗 CLI Pipe Mode & Chaining

`Please` can be used programmatically to build narratives via shell scripts or external agents.

### Add a node via Pipe
```bash
echo "As the fog cleared, a mysterious figure appeared." | please --pipe
```
*Outputs the new Node ID.*

### Chain messages
```bash
ROOT_ID=$(echo "You are a detective in 1920s London." | please --pipe --role system)
MSG_ID=$(echo "I want to investigate the docks." | please --pipe --parent $ROOT_ID)
please -c --jump $MSG_ID
```

### One-shot Generation
```bash
please --pipe --message "Tell me a joke about Go." --generate
```
*Appends the joke to your history and outputs the assistant's node ID.*

---

## 🚀 Getting Started

### Prerequisites
*   **Go:** 1.26 or higher.
*   **Ollama:** A local Ollama instance running with your preferred model (default: `gemma4:e4b`, though `gemma4:26b` is recommended for superior performance).

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
| `/q` or `/bye` | Gracefully exit the application. |

**Navigation Tips:**
*   **Scroll:** Use `↑`/`↓` or `PgUp`/`PgDn` to navigate the chat history.
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
