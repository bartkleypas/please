---
type: Concept
title: Context Resonance Scoring
description: Details of the Context Resonance Scoring algorithm used to prune old tool outputs and maintain API performance.
tags:
- algorithm
- tokens
- context
- pruning
timestamp: '2026-07-05T14:31:00-07:00'
---

# Context Resonance Scoring

To combat context window bloat during long conversational sessions—especially when executing verbose terminal commands or file edits—`Please` implements a dynamic **Context Resonance Scoring** algorithm that automatically scales to the model's configured context window (`num_ctx`).

---

## The Decay Model

The importance of each node on the active conversational path is evaluated using a time-decay exponential equation:

$$V = (W \cdot C) \cdot e^{-k \cdot \Delta t}$$

Where:
*   **$V$ (Resonance Value)**: The computed score indicating the relevance of a node to the current context.
*   **$W$ (Weight)**: Custom multipliers based on node significance (e.g. system commands, bookmarks, or manual user overrides have higher baseline weights).
*   **$C$ (Context Cost)**: The character/token size of the raw message and its observations.
*   **$e^{-k \cdot \Delta t}$ (Temporal Decay)**: An exponential decay factor where $\Delta t$ is the age of the node (turns and minutes since creation) and $k$ is a damping coefficient.

---

## Dynamic Budget-Aware Capacity Zones

Rather than applying rigid constant cutoffs, `Please` computes the active path's token load relative to the model's configured context window (`fillRatio = estimatedTokens / numCtx`):

1. **Healthy Capacity (`fillRatio < 60%`)**:
   * **100% Full Fidelity**: All historical nodes retain their complete chain-of-thought reasoning (`<thought>`) and full tool observation payloads (up to 8,000 characters).
   * Prevents premature "reasoning collapse" and memory loss on large-window models (such as Gemma 4 with 128k context).

2. **Moderate Load (`60% <= fillRatio < 85%`)**:
   * **Proportional Grace Window**: Automatically expands `graceTurns` to at least 50% of the active conversational path (`max(5, int(0.5 * pathLength))`).
   * Distant tool observations are gently trimmed to 2,000 characters while recent turns stay in full fidelity.

3. **High Pressure (`fillRatio >= 85%`)**:
   * Engages protective decay to prevent context exhaustion before the user triggers `/compact`.
   * Distant tool observations are crushed to 1-line summary skeletons (*"[Tool 'read_file' execution completed. Detailed results omitted. Total size: 4500 bytes.]"*), while the most recent 3 turns stay in full fidelity.

---

## Ephemeral Reasoning Tokens vs. Vault Preservation

* **Vault & UI Preservation (100% Recorded)**: Chain-of-thought reasoning (`<think>` / `node.Thought`) is permanently stored (and encrypted) in SQLite and displayed as interactive collapsible accordions (`▶ Thought Process • [Tab to expand]`) in the TUI and Web UI.
* **Prompt Context Sanitization (Clean Slate)**: When constructing the message context for the LLM (`BuildLLMContext`), historical reasoning tokens are stripped from prior assistant turns. Reasoning models (e.g. Gemma 4, DeepSeek-R1) treat reasoning tokens as *ephemeral generation scratchpads*; feeding past thoughts back into the model's auto-regressive context causes attention pollution, suppresses fresh reasoning, and induces model collapse. Observation results, tool call linkages, and signats are fully preserved.

