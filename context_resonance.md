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

To combat context window bloat during long conversational sessions—especially when executing verbose terminal commands or file edits—`Please` implements a dynamic **Context Resonance Scoring** algorithm.

---

## The Decay Model

The importance of each node on the active conversational path is evaluated using a time-decay exponential equation:

$$V = (W \cdot C) \cdot e^{-k \cdot \Delta t}$$

Where:
*   **$V$ (Resonance Value)**: The computed score indicating the relevance of a node to the current context.
*   **$W$ (Weight)**: Custom multipliers based on node significance (e.g. system commands, bookmarks, or manual user overrides have higher baseline weights).
*   **$C$ (Context Size)**: The character/token size of the raw message.
*   **$e^{-k \cdot \Delta t}$ (Temporal Decay)**: An exponential decay factor where $\Delta t$ is the age of the node in the conversation (number of turns since creation) and $k$ is a damping coefficient.

---

## Surgical Pruning of Tool Outputs

As nodes age ($\Delta t$ increases), their resonance score $V$ drops. 

When the score falls below a set threshold, the engine prunes the node content prior to sending the context history to the LLM:
1.  **Reasoning Blocks**: Strips internal `<thought>` or chain-of-thought blocks.
2.  **Tool Output Payload**: Truncates massive stdout logs or file diffs, replacing them with a concise, high-level summary (e.g. *"[Tool executed: git diff - 45 lines pruned]"*).
3.  **Core Dialogue Retention**: Retains the core user request and model responses, ensuring the narrative flow remains intact.

This prevents the LLM from paying high token costs for old command outputs while preserving the overall conversational history.
