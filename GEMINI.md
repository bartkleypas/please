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

*   **Current Focus**: Formalizing the multi-platform Swift Package evolution (`PleasePackage`) targeting iPad Mini (iPadOS) and macOS, while establishing an Intel Mac development bridge strategy ([ADR 004](decisions/004-swift-apple-ecosystem-evolution.md)).
*   **Go Baseline**: TUI input event routing and CLI pipe parsing stabilized ([2026-07-08](log.md#2026-07-08)).

---

## ⚖️ Active Debates

*   *There are currently no active debates.* The Swift port strategy and Intel-to-iPad bridge architecture have been resolved in [ADR 004](decisions/004-swift-apple-ecosystem-evolution.md).

---

## 💻 Key Developer Reminders

To maintain the architectural conventions of the `please` codebase during edits:
*   **DAG Navigation**: Remember that the "current" view in the app is always a leaf node in a DAG. Reconstruct linear history by traversing parent pointers up to the root.
*   **LLM Provider Abstraction**: Maintain strict protocol boundaries (`LLMProvider`) so cloud HTTP streaming (Gemini/OpenAI on Intel Macs) and local Apple Silicon MLX/CoreML engines swap seamlessly.
*   **Testing Discipline**: Always run fast hermetic tests (`go test -count=1 ./...` and `make build`) for automated verification (~2s). Treat `PLEASE_LIVE_FIRE=1` (`make test-livefire`) as **Manual / On-Demand Integration Testing** triggered only when explicitly evaluating real local LLM inference.

---

## 🗺️ Next Up (Roadmap)

- [ ] **Phase 5: Secure Execution Sandbox & Swift Package Foundation**
  - [ ] Implement command execution validation and safety rules.
  - [ ] Scaffold `PleasePackage` Swift structure (`PleaseEngine`, `PleaseUI`, `PleaseCLI`).
  - [ ] Build `CloudLLMProvider` using `URLSession` for testing on Intel Mac iPadOS Simulator.
  - [ ] Implement iPad Mini SwiftUI adaptive layout (Split View, Slide Over, Touch/Pencil graph navigation, `⌘ + Enter` shortcuts).
