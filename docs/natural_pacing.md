---
type: Concept
title: Natural Reading Pace Streaming
description: Details of the punctuation-sensitive stream pacing algorithm and TUI bypass handlers.
tags:
- tui
- streaming
- buffering
- user-experience
timestamp: '2026-07-05T14:31:00-07:00'
---

# Natural Reading Pace Streaming

To create a more immersive and natural dialogue experience, `Please` supports buffering and pacing the display of LLM streaming responses.

---

## Pacing Algorithm

Rather than printing stream chunks immediately as they arrive from Ollama or OpenAI, the engine intercepts the incoming data channel and feeds it into a timed rendering queue:

1.  **Incoming Buffer**: Tokens are held in a FIFO text buffer in the Bubble Tea state model.
2.  **Punctuation-Sensitive Pauses**: The TUI update loop releases text at a controlled character-per-second base rate, adding deliberate delay ticks when specific punctuation marks are encountered:
    *   **Periods (`.`), Exclamation marks (`!`), Question marks (`?`)**: Adds a $300\text{ ms}$ pause to simulate sentence transitions.
    *   **Commas (`,`), Semicolons (`;`), Colons (`:`)**: Adds a $100\text{ ms}$ pause for natural clauses.
3.  **Visual Immersion**: This results in a smooth, typing-like flow that mimics human speaking or reading pace rather than erratic API chunk dumps.

---

## Interaction Handlers & Bypass

Because forced delays can become frustrating when rapid feedback is needed, the TUI loop integrates instant bypass handlers:

*   **Bypass Inputs**: Pressing `ESC`, `Enter`, or `Space` triggers an immediate bypass command.
*   **Buffer Flushing**: The update loop immediately clears the timer, flushes all remaining buffered tokens to the viewport, and completes the node update state.
*   **Deferred Executions**: Any actions that depend on the completion of the text stream (such as node SQLite saving and subsequent tool execution triggers) are safely deferred until either the pacing finishes naturally or the user activates the bypass.
