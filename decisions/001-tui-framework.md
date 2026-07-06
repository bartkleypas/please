---
type: Decision
title: Selection of Bubble Tea & Lipgloss TUI Frameworks
description: Decision to use Charm's Bubble Tea and Lipgloss libraries to build a responsive, Elm-architecture-based terminal user interface.
tags:
- adr
- architecture
- tui
- go
- bubbletea
timestamp: '2026-07-05T14:31:00-07:00'
---

# ADR 001: Selection of Bubble Tea & Lipgloss TUI Frameworks

## Status
🏁 Approved

## Context
The MUC predecessor used standard CLI line reads and terminal print loops. Because `Please` represents conversations as a Directed Acyclic Graph (DAG), the user interface needs to render real-time interactive tree maps, support custom keyboard mappings (like Vim navigation), handle streaming updates dynamically, and display status overlays.

---

## Decision
1.  **UI Loop (Bubble Tea)**: Adopt Charm's **Bubble Tea** library (`github.com/charmbracelet/bubbletea`) to structure TUI loops using the Elm architecture (Model, Update, View). This provides clean, event-driven state updates for keyboards, tickers, and channel streams.
2.  **Styling (Lipgloss)**: Use **Lipgloss** (`github.com/charmbracelet/lipgloss`) to construct colors, borders, borders padding, and adaptive terminal layouts.
3.  **Command Execution**: Run LLM interactions and background streams asynchronously by wrapping them in Bubble Tea `tea.Cmd` handlers to prevent blocking the UI render cycle.
