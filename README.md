# 🦉 The Please Application

`Please` is a lightweight terminal user interface (TUI) application designed for fast, dynamic interaction with Large Language Models (LLMs).

Unlike linear chat applications, `Please` treats conversations as a Directed Acyclic Graph (DAG), allowing you to branch reality, jump between timelines, and maintain a persistent history stored in JSONL format. The TUI provides a robust navigation suite: use `/map` to visualize the branching paths of your story, `/jump <id>` to teleport to specific nodes in the timeline, and `/bookmarks` to manage saved "anchors" in the narrative tapestry.

---

## ✨ Features

### 🏗️ Engine Architecture
* **DAG-Based Navigation:** Breaks the linear chat mold by allowing users to branch, prune, and jump across multiple conversation timelines.
* **Hybrid Vault:** Secure, optionally encrypted (AES-256) storage of narrative history in a local JSONL format.

### 🎭 Narrative Interaction
* **TUI Interface:** A rich, TUI experience with color-coded feedback and intuitive command handling.

---

## 🤖 Runtime Commands

While in in the app, the following commands allow you to navigate and execute commands within the existing story:

| Command | Category | Description |
| :--- | :--- | :--- |
| `/help` | System | View the command list. |
| `/map` | Navigation | Display the visual tree of the DAG. |
| `/jump <id>` | Navigation | Pivot to a "fuzzy find" node ID in the narrative graph. (must be assistant turn) |
| `/mark <str>` | Navigation | Place a named bookmark at the current location. |
| `/bookmarks` | Navigation | List all saved bookmarks in the tree. |
| `/export <fileName>.jsonl` | System | Append the (decrypted) current conversation to the targetted JSONL training file. |
| `/q` or `/bye` | System | Gracefully exit, restoring the previous terminal state. |

---

## 🦎 The Forge: From Dialog to LoRA

The `Please` app is designed not just for play, but for the deliberate cultivation of a Sovereign Persona. By utilizing the apps built in features, the user can "harvest" specific conversations from the narrative graph into a dataset useful in fine-tuning via something like Axolotl.

1. **Image curation via `/mark`:** To prevent storage and computational overhead while ensuring high-signal data capture, the apps `LogManager` functionality uses an intentional gating system allowing the user to:
   * **Command:** Use `/mark <label>` to distinguish a specific node in the graph.
2. **Harvesting via `/export`:** When a narrative branch demonstrates a specific "High resonance" quality, it can be flattened into a **ChatML** multi-turn training sample.
   * **Command:** `/export <fileName>.jsonl`
3. **Dataset Stacking:** The `/export` command will append the current conversation to the targetted file. This allows the user to stack multiple distinct timelines into a single training file, creating a robust dataset of diverse reactions and emotional states, perfect for feeding into Axolotl training pipelines.
