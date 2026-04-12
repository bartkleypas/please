# 🗺️ Development Plan: Project "Please"

This document outlines the phased approach to building the `Please` TUI application. The core objective is to move away from linear chat and implement a Directed Acyclic Graph (DAG) for conversation management.

---

## 🛠️ Development Phases

### Phase 1: The Narrative Engine (Data Layer) [DONE]
Before building the UI, we must define how the "universe" is stored and traversed.
*   **Data Modeling:** Define the `Node` struct (ID, ParentID, Role, Content, Timestamp) and the `Graph` manager.
*   **Persistence:** Implement the JSONL reader/writer.
*   **DAG Logic:** Create methods to retrieve a "linear path" from the root to any given node ID (essential for providing context to the LLM).
*   **Encryption:** Implement the AES-256 wrapper for the vault.
*   **Milestone 1:** A CLI tool that can save a branching conversation to a file and reload it into memory.
*   **Testing:** Unit tests for DAG traversal and encryption/decryption integrity.

### Phase 2: TUI Foundation (Bubble Tea) [DONE]
Setting up the interactive shell.
*   **Model Definition:** Define the Bubble Tea `Model` to track the current node, input state, and view mode.
*   **Command Parser:** Implement a handler for the `/` commands (e.g., `/jump`, `/map`).
*   **Basic Rendering:** A scrollable viewport to display the current conversation timeline.
*   **Milestone 2:** A functional TUI where you can type messages and see them echoed back in a simulated chat.
*   **Testing:** Integration tests for the command parser to ensure `/command` is distinguished from regular text.

### Phase 3: LLM Integration & Branching [DONE]
Connecting the engine to a brain.
*   **API Client:** Implement a provider interface (supporting OpenAI, Anthropic, or Local LLMs via Ollama).
*   **Context Assembly:** Logic to flatten the DAG path into a prompt the LLM understands.
*   **Branching Logic:** Implement the ability to "jump" back to a previous node and send a new message, creating a new branch.
*   **Milestone 3:** A live chat experience where you can branch the conversation and jump between timelines.
*   **Testing:** Mock API tests to ensure the TUI handles streaming responses and timeouts gracefully.

### Phase 4: Navigation & Curation Tools [DONE]
Implementing the "Sovereign Persona" features.
*   **The Map (`/map`):** A visual representation of the DAG (likely a tree-like text visualization).
*   **Bookmarks (`/mark`):** Logic to tag specific nodes and store them in a lookup table.
*   **The Exporter (`/export`):** A function to flatten a specific branch into the **ChatML** format for LoRA training.
*   **Milestone 4:** Ability to mark high-resonance nodes and export a clean `.jsonl` dataset.
*   **Testing:** Validation of the exported JSONL against ChatML specifications.

### Phase 5: Polish & Hardening [DONE]
*   **UX Improvements:**
  1. [X] Introduce "viewport" UI layout (high complexity)
  2. [X] Chat history in top boxed area (scrollable)
  3. [X] Pinned user input field at the bottom that grows vertically if required for long inputs
  4. [X] Loading spinners for LLM responses ("thinking" spinner).
  5. [X] `/map` and other commands that require UI output use the "chat history" area for display
  6. [X] Adding colors ("system", "user", and "agent" colors).
*   **Graceful Exit:** Ensuring the terminal state is restored on `/q` or `/bye`.
*   **Final Stress Test:** Testing with very large conversation graphs to ensure performance doesn't degrade.
*   **Milestone 5:** Production-ready TUI.

### Phase 6: Nice to have (Refinements) [IN-PROGRESS]
*   **Persistant User Files:** Choose a location for the vault/journal and other configuration details. [DONE]
  * `(OS specific config dir)/please/config.json` - Config details.
  * `$HOME/.local/share/please/vault.jsonl` - Running history, and what is resumed on app launch.
*   **Root node at system prompt:** If we wish to provide a system prompt for the graph to build on, it is currently difficult to do so. Proposed: Re-flow dialog tree construction from a `role="system"` root node, with the first user turn directly afterwords. Might complicate chatml export. I don't actually think we need to support exporting to chatml files. We don't really want to with the `please' utility. It doesn't "vibe" with the tools goals. We can trim that functionality later.
*   **Remove chatML export functionality:** Don't forget to clean up the `/export` command too.
*   **System prompt library?:** No idea if it would be possible, but came up in testing. User comment: "It would be nice if i could have a different assistant persona for code versus emails." Possible complexity explosion? Let's discuss.
*   **Encryption:** Let's discuss our options. (Enhancemet)

---

## 🧪 Summary of Testing Milestones

| Milestone | Focus | Primary Test Method |
| :--- | :--- | :--- |
| **1. Engine** | Data Integrity | Unit Tests (Go `testing` package) |
| **2. TUI** | Input/Output | Manual UI testing + Command Parser tests |
| **3. LLM** | Connectivity | Mock API responses / Integration tests |
| **4. Curation** | Data Export | Schema validation of exported JSONL |
| **5. Final** | Stability | End-to-end "User Journey" testing |
