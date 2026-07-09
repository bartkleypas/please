---
type: Concept
title: "Please: Agent Bootstrap Memory"
description: "Immediate orientation anchor for developers and AI agents waking up in the please project."
tags:
  - please
  - go
  - bootstrap
timestamp: "2026-07-09T14:35:00-07:00"
---

# Please: Agent Bootstrap Memory

> [!IMPORTANT]
> **Attention Agents / Context Windows:** This file serves as the system "bootstrap" memory. Read this first to understand the *immediate* state of the project, open debates, and active tasks.

---

## 🗺️ Project Navigation Hub

For detailed and authoritative documentation, consult these dedicated files instead of relying on this bootstrap anchor:
*   [Project Index](index.md) - The central directory of packages, concepts, and specifications.
*   [Technical README](README.md) - Features list, CLI examples, setup, TUI controls table, and testing instructions.
*   [Context Resonance Spec](context_resonance.md) - Math formula and scoring mechanics for token decay.
*   [Natural Pacing Spec](natural_pacing.md) - Stream buffering and punctuation-sensitive pacing rules.
*   [Architecture Decisions (ADRs)](decisions/index.md) - Historical record of TUI framework, SQLite storage, and visualization server.
*   [Change Log](log.md) - Chronological ledger of all modifications.

---

## 🏗️ Active Design Context & Focus

*   **Current Focus**: Refactoring TUI input event routing to prevent double-capture bugs (e.g. key captures when viewport overrides are active) and stabilizing CLI pipe vs positional argument parsing (completed [2026-07-08](log.md#2026-07-08)).
*   **Next Steps**: Refining tool-calling capabilities and permissions within the harness.

---

## ⚖️ Active Debates

*   *There are currently no active debates.* All historical debates have been resolved and moved to [decisions/index.md](decisions/index.md).

---

## 💻 Key Developer Reminders

To maintain the architectural conventions of the `please` codebase during edits:
*   **DAG Navigation**: Remember that the "current" view in the app is always a leaf node in a DAG. Reconstruct linear history by traversing parent pointers up to the root.
*   **Bubble Tea Event Loop**: LLM responses are streamed via Go channels into the Bubble Tea loop. Deferred persistence and tool execution trigger *only* after pacing ticks complete or are skipped by the user.

---

## 🗺️ Next Up (Roadmap)

- [ ] **Phase 5: Secure Execution Sandbox**
  - [ ] Implement command execution validation and safety rules.
  - [ ] Add interactive permissions prompt before allowing tools like `ssh` to run.
