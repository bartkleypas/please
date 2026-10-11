---
type: Concept
title: "Cache-Stable Context Shaping & Smart Receipt Paging"
description: "Details of the ContextShaper mathematical projections (Sigmoid, Window, Resonance), Monotonic Prefix Invariance, and SmartReceipt out-of-band telemetry paging (ADR-020, ADR-021)."
tags:
  - algorithm
  - tokens
  - context
  - shaper
  - receipts
  - kv-cache
timestamp: "2026-10-11T10:24:00-07:00"
---

# Cache-Stable Context Shaping & Smart Receipt Paging

To maintain high reasoning density and instant prompt prefill across long engineering sessions—especially when executing verbose terminal commands or multi-file refactors—`Please` decouples prompt construction from database persistence via the **`ContextShaper`** interface ([ADR-020](../decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md)) and **Tiered Sensory Smart Receipts** ([ADR-021](../decisions/021-tiered-sensory-storage-and-observation-compaction.md)).

---

## 1. Pure Read-Only Prompt Projections (`ContextShaper`)

Prompt assembly in [`internal/engine/shaper.go`](../internal/engine/shaper.go) is a **pure, read-only mathematical projection** with zero side effects or database mutations:

$$\text{DAG Path} \times \text{Token Budget} \longrightarrow \text{[]domain.Message}$$

### Why Wall-Clock Time Was Retired
Early versions of `Please` used a wall-clock exponential decay equation ($V = (W \cdot C) \cdot e^{-k \cdot \Delta t}$ where $\Delta t = \text{time.Since(node.Timestamp)}$). In `v0.3.0` ([ADR-020](../decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md)), wall-clock time was completely eliminated from context shaping for two reasons:
1. **No "Lunch Break Amnesia"**: Stepping away from the terminal for 45 minutes or resuming a session the next day must never degrade the conversation's fidelity score when zero new turns have occurred.
2. **No "Shivering Tokens" (KV-Cache Stability)**: Continuous clock ticks caused historical nodes to re-truncate at shifting character offsets on every turn, invalidating prefix-based KV caches in Ollama, llama.cpp, vLLM, and cloud providers.

Instead, `ContextShaper` evaluates node fidelity strictly using **Topological Turn Distance ($\Delta d$)** (turns from the active playhead leaf) and **Capacity Pressure (`fillRatio = estimatedTokens / num_ctx`)**.

---

## 2. Monotonic Prefix Invariance (The KV-Cache Contract)

To guarantee near-instant prompt prefill reuse across consecutive turns:
* **PINNED Genesis Root (`idx == 0` & Initial User Goal `idx == 1`)**: The system prompt (`RoleSystem`, including any layered Signat, Ambient Telemetry, or Cybernetic Memory orientation overlays) and the initial user goal turn are permanently pinned at `FidelityFull` (100% fidelity).
* **Active Working Plateau**: Recent turns near the playhead leaf ($\Delta d = 0 \dots d_0$) retain full fidelity.
* **The Frozen Asymptote (Stable Floor)**: Once an aging ancestor node crosses the roll-off horizon and settles onto `FidelityLow`, its rendered text representation is **frozen**—it never shrinks by further characters on subsequent turns, keeping the token prefix $0 \dots M$ cache-valid.

---

## 3. Selectable Shaper Curves (`sigmoid | window | exponential`)

Operators can select the active projection strategy via `server.context_shaper` in `config.json`:

### A. `SigmoidShaper` (`"sigmoid"`, Recommended Default)
Applies a logistic S-curve over topological turn distance $d$:

$$S(d) = \frac{1}{1 + e^{k(d - d_0)}}$$

*(Default parameters: slope $k = 0.8$, inflection horizon $d_0 = 6.0$ turns).*
* **Healthy Capacity (`fillRatio < 60%`)**: All public nodes on the path retain `FidelityFull`.
* **Active Plateau ($S(d) \ge 0.50$)**: Full dialogue and recent observations remain intact.
* **Transition Zone ($0.15 \le S(d) < 0.50$)**: Enters `FidelityMedium`.
* **Stable Floor ($S(d) < 0.15$)**: Settles onto `FidelityLow` (spoken dialogue preserved; historical tool observations projected as compact **Smart Receipts**).

### B. `WindowShaper` (`"window"`)
Enforces a deterministic $K$-turn sliding window (default `WindowSize = 10` turns) plus the pinned Genesis root and initial user goal. Turns older than $K$ steps immediately settle onto `FidelityLow`. Ideal for tight memory budgets and maximum predictability.

### C. `ResonanceShaper` (`"exponential"`)
The refactored topological resonance curve stripped of wall-clock time:

$$V(d) = (W \cdot C) \cdot e^{-k \cdot d}$$

Scales grace windows dynamically by capacity zone (`< 60%` full fidelity, `60%–85%` proportional grace window, `>= 85%` high-pressure compaction).

---

## 4. Tiered Sensory Storage & Smart Receipts ([ADR-021](../decisions/021-tiered-sensory-storage-and-observation-compaction.md))

Spoken dialogue and tool observations have fundamentally different entropy curves. While user and assistant dialogue contains enduring goals and contracts, raw tool stdout/file reads can easily span tens of kilobytes.

1. **Out-of-Band Blob Storage (`observation_blobs`)**: When `SessionHarness` executes a tool, raw output is compressed (gzip) and persisted out-of-band in the SQLite `observation_blobs` table keyed by a content-addressable hash (`obs_<8-hex-chars>`).
2. **In-DAG Smart Receipts**: Every `domain.ToolObservation` carries a structured `SmartReceipt` alongside its result:
   ```text
   [Receipt obs_94a2f8b1 | execute_command "go test ./..." | exit=1 | 142 lines, 8420 bytes | FAIL: TestSessionHarness_Compaction (0.12s) | Use inspect_receipt to view full output]
   ```
3. **Asymmetric Projection**: On historical turns (`distance >= 2` or `FidelityLow`), `ContextShaper` replaces verbose multi-kilobyte tool outputs with their 1-line deterministic `SmartReceipt`, slashing prompt token overhead by 90%+ while keeping the exact failure banner and receipt ID visible.
4. **On-Demand Virtual Paging (`inspect_receipt`)**: Whenever an agent needs to inspect exact lines or stack traces from an older receipt, it calls `inspect_receipt(receipt_id="obs_94a2f8b1", offset=1, limit=100, grep="FAIL")` to page only the relevant lines into its active forward turn.

---

## 5. Ephemeral Inter-Turn vs. Intra-Turn Reasoning (`<thought>`)

* **Vault & UI Preservation (100% Recorded)**: Chain-of-thought reasoning (`node.Thought`) is permanently saved (and AES-256-GCM encrypted when configured) in SQLite and rendered as collapsible accordions (`▶ Thought Process • [Tab to expand]`) in the TUI.
* **Intra-Turn Working Memory Continuity**: Within a single multi-step turn (`SessionHarness.ExecuteTurn`), intermediate reasoning segments remain attached to in-flight assistant messages so reasoning models (e.g. Gemma 4, DeepSeek-R1) maintain their step-by-step plan across consecutive tool calls without looping.
* **Inter-Turn Clean Slate**: Once a turn concludes and becomes a historical ancestor node, `ContextShaper` strips old `Thought` scratchpads from prior turns so historical reasoning noise never pollutes fresh generation passes.
