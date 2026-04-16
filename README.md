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
| `/q` or `/bye` | System | Gracefully exit, restoring the previous terminal state. |
