---
type: Concept
title: "Package internal/engine Index"
description: "Core orchestration layer: canonical SessionHarness turn lifecycle, ContextShaper projections, SmartReceipt synthesis, subagent delegation, and DAG management (ADR-018, ADR-019, ADR-020, ADR-021, ADR-022)."
tags:
  - please
  - go
  - engine
  - harness
  - shaper
  - index
timestamp: "2026-10-11T10:23:00-07:00"
---

# Package internal/engine Index (`internal/engine`)

The `internal/engine` package sits at **Layer 2 (Core Orchestration & Lifecycle)** of the four-tier architecture ([ADR-018](../../decisions/018-package-stratification-and-domain-decoupling.md)). It coordinates the canonical multi-step conversational turn loop, cache-stable prompt projections, tiered observation receipts, sub-session agent delegation, and DAG maintenance across standalone TUI, headless daemon, and ACP surfaces.

## Core Files & Components

### 1. Canonical Execution Lifecycle ([ADR-019](../../decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md))
* [harness.go](harness.go) - Declares `SessionHarness`, the single authoritative execution runner for conversational turns (`ExecuteTurn`). Orchestrates context shaping, streaming LLM generation, intra-turn thought continuity, interactive `PermissionGate` pauses, tool execution, out-of-band blob persistence, and session head advancement. Emits typed `HarnessEvent` frames (`token`, `thought`, `tool_call`, `tool_result`, `node_complete`, `error`).
* [local_provider.go](local_provider.go) - Implements `LocalHarnessProvider`, adapting `SessionHarness` to `providers.LLMProvider` so the standalone Bubble Tea TUI executes turns through the exact same harness pipeline as remote daemon clients, bridging interactive tool approvals via `ApprovalReqChan`.
* [service.go](service.go) - Declares `Manager`, coordinating the in-memory `graph.Graph`, `storage.Storage`, `tools.Registry`, `ContextShaper`, workspace cloning (`CloneWithWorkspace`), pure DAG supernode compaction (`CompactRange`, `CompactRangeWithDirective`), and session-guarded branch pruning (`PruneBranch`).

### 2. Cache-Stable Context Shaping ([ADR-020](../../decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md))
* [shaper.go](shaper.go) - Defines the pure read-only `ContextShaper` interface (`ShapeContext(ctx, path, budget)`) and standard implementations:
  * `SigmoidShaper` (default): Logistic S-curve driven by topological step distance ($\Delta d$) and token fill ratio with a pinned Genesis root and frozen historical asymptote for monotonic KV-cache prefix invariance.
  * `WindowShaper`: Strict $K$-turn sliding window projection.
  * `ResonanceShaper`: Refactored topological exponential decay (with zero wall-clock time dependency).

### 3. Tiered Sensory Receipts & Cybernetic Memory Adapters ([ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md), [ADR-021](../../decisions/021-tiered-sensory-storage-and-observation-compaction.md))
* [receipt.go](receipt.go) - Synthesizes deterministic, content-addressable `domain.SmartReceipt` (`obs_<hash>`) summaries and 1-line diagnostic banners from raw tool outputs, and bridges `tools.ReceiptStore` to `storage.ObservationBlobStore`.
* [memory_adapter.go](memory_adapter.go) - Bridges `storage.MemoryStore` to `tools.MemoryProvider` (`toToolMemoryItem`), wiring persistent L3 cybernetic memory recall, storage, deletion, and diagnostics into the active tool registry.

### 4. Native Delegated Multi-Agent Topologies ([ADR-022](../../decisions/022-native-delegated-multi-agent-topologies.md))
* [delegate.go](delegate.go) - Implements `engineSubagentRunner` (`RunSubagent`, `InspectSubagent`, `ReconcileSubagent`). Provisions isolated child sessions (`sub_<uuid>`), mounts out-of-tree Git worktrees (including nested `target_dir` repositories), enforces single-turn `max_steps` budgets and depth-1 anti-recursion, and returns synthesized findings to the parent turn.

### 5. Tool Parsing, Signats & Peripheral Telemetry
* [tool_defaults.go](tool_defaults.go) - Registers default workspace tools, memory adapters, receipt stores, and subagent runners onto `Manager`.
* [tool_extract.go](tool_extract.go) - Extracts fallback tool calls from raw LLM text/XML/markdown blocks and normalizes tool call IDs.
* [signat.go](signat.go) - Derives semantic 2-emoji turn signatures (`ExtractSignat`, `deriveSilentSignat`) and manages the optional `SignatSteeringContract` overlay.
* [telemetry.go](telemetry.go) - Derives bounded peripheral telemetry (`DeriveAmbientTelemetry`: `cwd`, `git_branch`, `local_time`, `index_file`, editor context) and wraps leaf user prompts in `<USER_REQUEST>` / `<ADDITIONAL_METADATA>` XML envelopes ([ADR-003](../../decisions/003-ephemeral-leaf-telemetry-pure-root-persona.md)).
* [version.go](version.go) - Canonical runtime version and git commit variables injected via `ldflags`.

### 6. Hermetic & Living Scenario Test Suites ([ADR-015](../../decisions/015-living-scenarios-and-test-subsystem-decomposition.md))
* [scenarios_test.go](scenarios_test.go) - Multi-turn Living Scenarios validating end-to-end tool loops, receipt paging, and subagent delegation.
* [harness_test.go](harness_test.go), [shaper_test.go](shaper_test.go), [receipt_test.go](receipt_test.go), [delegate_test.go](delegate_test.go), [memory_adapter_test.go](memory_adapter_test.go), [local_provider_test.go](local_provider_test.go), [service_test.go](service_test.go), [signat_test.go](signat_test.go), [telemetry_test.go](telemetry_test.go), [tool_extract_test.go](tool_extract_test.go), & [tool_test.go](tool_test.go) - Comprehensive unit test coverage.
