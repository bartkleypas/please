---
type: Concept
title: "Package internal/tui Index"
description: "Charm Bubble Tea TUI built on a hierarchical ViewStack, modal focus layers, DAG tree visualizer, cybernetic memory deck, phonic bell staging, and natural token pacing (ADR-001, ADR-013, ADR-017, ADR-019)."
tags:
  - please
  - go
  - tui
  - bubbletea
  - viewstack
  - index
timestamp: "2026-10-11T10:23:00-07:00"
---

# Package internal/tui Index (`internal/tui`)

The `internal/tui` package (Layer 3: Presentation) implements the interactive Terminal User Interface for `Please`, powered by Charm's **Bubble Tea** and **Lipgloss** ecosystems ([ADR-001](../../decisions/001-tui-framework.md)).

Per [ADR-017](../../decisions/017-tui-viewstack-and-hierarchical-focus-architecture.md) and [ADR-019](../../decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md), the TUI operates as a pure, reactive presentation consumer over `engine.LocalHarnessProvider` (or `providers.RemoteDaemonProvider`) and routes all keystrokes and rendering through a composable **`ViewStack`** of focus-isolated layers.

## Core Files & Components

### 1. ViewStack & Layer Compositing ([ADR-017](../../decisions/017-tui-viewstack-and-hierarchical-focus-architecture.md))
* [view_stack.go](view_stack.go) - Implements `ViewStack` and the `Layer` interface (`ID`, `Update`, `View`, `IsModal`, `Title`, `FooterHints`). Routes `tea.KeyMsg` strictly to the top active layer to prevent keystroke leakage.
* [chat_layer.go](chat_layer.go) - Base `ChatLayer` handling primary prompt input, history scrolling, thought folding (`Tab`), and slash command dispatch.
* [map_layer.go](map_layer.go) - Interactive `MapLayer` wrapping the `/map` DAG tree visualizer with Vim-style traversal (`h/j/k/l`, `g/G`), fuzzy search (`/`), compaction (`c`), and branch pruning (`d`).
* [memories_layer.go](memories_layer.go) & [memories_view.go](memories_view.go) - Interactive `/memories` card deck browser for inspecting, searching, and pruning L3 persistent cybernetic memories ([ADR-014](../../decisions/014-persistent-agent-memory-and-cybernetic-recall.md)).
* [text_layer.go](text_layer.go) - Scrollable modal overlay layer for `/help`, `/config`, `/list`, `/session`, and `/worktree` sheets.
* [confirm_overlay.go](confirm_overlay.go) - Interactive modal consent gate (`ToolConfirmOverlay`, `ConfirmOverlay`) pushed onto the `ViewStack` when `SessionHarness` pauses for human tool approval (`y/n/Esc`) or branch deletion confirmation.

### 2. Core Model, Initialization & Layout
* [tui.go](tui.go) - Initializer (`NewModel`) wiring `Manager`, configuration, session heads, and the base `ChatLayer` on the `ViewStack`.
* [model.go](model.go) - Declares the Bubble Tea `Model` struct, stream channels (`StreamContentChan`, `StreamThoughtChan`, `StreamToolCallChan`, `StreamApprovalReqChan`), pacing buffers, and viewport dimensions.
* [view.go](view.go) - Top-level frame compositor rendering the header bar (session badge, connection status), active `ViewStack` layer, status notifications, and security policy footer (`[🔒 STRICT]`, `[🛡️ STANDARD]`, `[⚠️ PERMISSIVE]`).
* [styles.go](styles.go) - Lipgloss color palettes, borders, role badges, and layout constants.

### 3. Chat Rendering, Map & Compaction
* [chat.go](chat.go) & [messages.go](messages.go) - Renders multi-turn conversation transcripts, collapsible `<thought>` accordions (`▶ Thought Process • [Tab to expand]`), tool call/receipt badges, and markdown bodies.
* [map.go](map.go) - Renders the ASCII/Unicode DAG tree-map with emoji signats, active lineage highlighting, and bookmark indicators.
* [navigation.go](navigation.go) - Implements graph traversal helpers and "Rewind & Edit" user turn navigation (`navigateToNode`).
* [compaction.go](compaction.go) - TUI handlers for triggering range compactions (`RoleSummary` Supernodes) and updating viewport lineage.

### 4. Streaming, Phonic Staging & Event Handlers
* [streaming.go](streaming.go) - Manages real-time token/thought stream consumption, punctuation-sensitive natural reading pacing ([docs/natural_pacing.md](../../docs/natural_pacing.md)), and turn finalization.
* [bell.go](bell.go) - Implements Phonic Staging ([ADR-013](../../decisions/013-acoustic-theatrics-phonic-staging-and-talon-tap-telemetry.md)) via `BellCmd()` (`\a` / ASCII `0x07`), firing strictly when human attention is required (turn completion, error, or interactive `PermissionGate` yield).
* [handlers.go](handlers.go) - Handles spinner ticks, export/sync notifications, and `handleToolApprovalReq` (pushing `ToolConfirmOverlay` onto the `ViewStack`).
* [events.go](events.go) - Consumes remote daemon SSE bus events (`EventNodeSaved`, `EventBranchPruned`, `EventSupernodeCreated`) with camera decoupling ([ADR-007](../../decisions/007-multi-session-daemon-branch-concurrency.md)).
* [commands.go](commands.go) - Slash command dispatcher (`/help`, `/map`, `/memories`, `/session`, `/worktree`, `/sandbox`, `/bell`, `/pacing`, `/persona`, `/config`, `/jump`, `/mark`, `/gc`, `/audit`).
* [keys.go](keys.go) - Global interrupt and keybinding helpers.
* [tea_cmds.go](tea_cmds.go) & [utils.go](utils.go) - Asynchronous `tea.Cmd` generators (`waitForStream`, `generateLLMResponse`) and text formatting utilities.

### 5. Test Suites
* [tui_test.go](tui_test.go), [view_stack_test.go](view_stack_test.go), [confirm_overlay_test.go](confirm_overlay_test.go), [navigation_test.go](navigation_test.go), & [text_layer_test.go](text_layer_test.go) - Hermetic state machine, layer focus isolation, and modal approval test suites.
