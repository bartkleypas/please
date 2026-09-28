---
type: Decision
title: "017: TUI ViewStack, Hierarchical Focus Isolation, and Modal Compositing"
description: "Architecture decision record establishing a formal ViewStack for the Please terminal user interface, retiring scattered boolean modal flags, eliminating keystroke leakage across modal overlays, and standardizing reusable navigation traits (Vim j/k/g/G bindings)."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - tui
  - bubbletea
  - focus
  - viewstack
  - ux
timestamp: "2026-09-22T18:30:00-07:00"
---

# ADR 017: TUI ViewStack, Hierarchical Focus Isolation, and Modal Compositing

## Status

Accepted

---

## Context

The terminal user interface (TUI) of `please` began as a straightforward chat viewport and input bar powered by Charm's [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss) ([ADR 001](001-tui-framework.md)). Over subsequent milestones, the TUI expanded into a multi-modal operating cockpit:

1. **DAG Graph Exploration (`/map`)**: A scrollable viewport visualizing branching conversation trajectories, with node hopping and lineage inspection.
2. **Persistent Memory Deck (`/memories`)**: A browseable card deck and extended inspection card modal for browsing workspace and global memories ([ADR 014](014-persistent-agent-memory-and-cybernetic-recall.md)).
3. **Modal Confirmation Dialogs**: Interactive approval prompts for branch pruning, range compaction, and human-in-the-loop tool execution ([ADR 011](011-agent-sandboxing-execution-isolation.md), [ADR 012](012-agent-client-protocol-support.md)).
4. **Natural Turn Pacing Overlays**: Soft visual delays simulating typing cadences with manual skip keystrokes.
5. **Upcoming Interactive Configuration Overhaul**: A planned full-screen visual configuration manager with tabs, toggles, and inline field editing (ADR-019).

### The "Boolean Jungle" and Keystroke Leakage

Because Bubble Tea intentionally adheres to a minimalist Elm Architecture (`Init`, `Update`, `View`) without a prescribed window manager or screen navigation stack, modal state management accumulated organically directly on the root `Model` struct:

```go
// Current state distribution across tui.Model
ViewMode                       ViewMode             // ModeChat, ModeMap, ModeMemories
AwaitingCompactConfirmation    bool
AwaitingPruneConfirmation      bool
AwaitingToolConfirmation       bool
PacingActive                   bool
MemoryDetailCard               *storage.Memory
ViewportOverride               string
```

This arrangement exhibits several architectural liabilities:

* **Combinatorial State Space**: Five separate boolean flags, an enum, and two pointer variables yield dozens of theoretical state combinations. Determining which component is "on top" requires sprawling, brittle conditional ladders.
* **Keystroke Leakage**: Event routing in `handleKeyEvent` (`internal/tui/keys.go`) cascades through `handleModalKeys`, then `switch m.ViewMode`, and finally falls through to the chat `TextInput` and viewport. If a sub-view or modal forgets to explicitly mark a key as handled, keystrokes (such as `j`, `k`, `space`, or arrow keys) leak into the chat input buffer or trigger unexpected viewport scrolling.
* **High Barrier for New Views**: Adding any new full-screen view or modal overlay (such as the upcoming interactive Config View) requires editing at least four disparate files (`model.go`, `keys.go`, `view.go`, `commands.go`) and carefully wiring new mutual-exclusion checks against all existing boolean flags.
* **Inconsistent Navigation Bindings**: The Map view and Memories deck each independently re-implement arrow and Vim navigation (`j`/`k`, `g`/`G`), leading to slight behavioral divergences and duplicated code.

We need a unified, structural abstraction that manages **both visual layering (Z-order) and event routing (focus)** as a single coherent concept.

---

## Decision

We will implement a **`ViewStack`** architecture for the `please` TUI. 

Visual presentation ordering and keyboard input focus will be governed by a single, unified LIFO (Last-In, First-Out) stack of **`ViewLayer`** instances. The element at the top of the stack exclusively owns the keyboard event stream.

```
┌─────────────────────────────────────────────────────────────┐
│                    TUI ViewStack Model                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  [Top Layer]     Tool Confirmation Dialog                   │
│                  • Exclusively intercepts tea.KeyMsg (y/n)  │
│                  • Rendered as floating Lipgloss modal box  │
│                               ▲                             │
│                               │ Push / Pop                  │
│                               ▼                             │
│  [Middle Layer]  Memories Deck Layer                        │
│                  • State preserved, paused underneath       │
│                  • Owns j/k/g/G vim list navigation         │
│                               ▲                             │
│                               │ Push / Pop                  │
│                               ▼                             │
│  [Base Layer]    Chat Timeline Canvas (Root)                │
│                  • Permanent root anchor (never popped)     │
│                  • Viewport + multi-line prompt textarea    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 1. The `ViewLayer` Interface

Every full-screen surface, overlay deck, or modal dialog will implement the `ViewLayer` interface:

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// ViewLayer represents an isolated visual surface and event handler on the ViewStack.
type ViewLayer interface {
	// Name returns a human-readable identifier for diagnostics and telemetry.
	Name() string

	// Update handles Bubble Tea messages when this layer resides at the top of the stack.
	// Returns a command and a boolean indicating whether the message was consumed.
	Update(msg tea.Msg) (tea.Cmd, bool)

	// View renders the visual output of this layer for the specified dimensions.
	View(width, height int) string

	// IsOverlay returns true if this layer renders as a floating modal on top of
	// the underlying layer, or false if it completely replaces the screen.
	IsOverlay() bool
}
```

### 2. The `ViewStack` Implementation

The `ViewStack` coordinates push, pop, event dispatch, and composited rendering:

```go
type ViewStack struct {
	layers []ViewLayer
}

func NewViewStack(root ViewLayer) *ViewStack {
	return &ViewStack{layers: []ViewLayer{root}}
}

func (s *ViewStack) Push(layer ViewLayer) {
	s.layers = append(s.layers, layer)
}

func (s *ViewStack) Pop() ViewLayer {
	if len(s.layers) <= 1 {
		return nil // Permanent root layer cannot be popped
	}
	top := s.layers[len(s.layers)-1]
	s.layers = s.layers[:len(s.layers)-1]
	return top
}

func (s *ViewStack) Top() ViewLayer {
	if len(s.layers) == 0 {
		return nil
	}
	return s.layers[len(s.layers)-1]
}

func (s *ViewStack) Len() int {
	return len(s.layers)
}
```

### 3. Invariant Event Routing Contract

1. **Global Interceptors First**: Terminal resize (`tea.WindowSizeMsg`) and global emergency escape (`ctrl+c`) are intercepted before stack dispatch.
2. **Exclusive Top Dispatch**: All other messages (principally `tea.KeyMsg`) are forwarded directly to `stack.Top().Update(msg)`. 
3. **Structural Leak Prevention**: If a layer is active on top of the stack, underlying layers (specifically `TextInput` and `Viewport`) **never receive the message**. Keystroke leakage becomes structurally impossible.
4. **Universal Escape Convention**: If the top layer receives `esc` and does not claim custom handling (such as aborting an active multi-line text edit), the `ViewStack` automatically pops the top layer, instantly restoring the previous screen.

### 4. Composited Overlay Rendering

When rendering the screen in `Model.View()`:
* If `stack.Top().IsOverlay() == false`: The top layer renders full-screen.
* If `stack.Top().IsOverlay() == true`: The base layer (or layer immediately underneath) renders its background, and the top layer's content is composited over it using Lipgloss box placement (`lipgloss.Place` or centered floating dialog styling).

### 5. Reusable Navigation Traits (Vim Bindings)

To eliminate navigation inconsistencies across views, we will extract a reusable `ListNavigator` / `VimNavigation` helper:

```go
type ListNavigator struct {
	Cursor int
	Total  int
}

func (n *ListNavigator) HandleKey(key string) bool {
	switch key {
	case "up", "k":
		n.Prev()
		return true
	case "down", "j":
		n.Next()
		return true
	case "home", "g":
		n.First()
		return true
	case "end", "G":
		n.Last()
		return true
	}
	return false
}
```

Both `MapLayer`, `MemoriesDeckLayer`, and the upcoming `ConfigLayer` will embed or utilize this shared navigator, guaranteeing uniform keyboard ergonomics across the entire application.

---

## Phased Implementation Plan

1. **Phase 1: Foundation (This Branch - `kbartley/tui-view-stack`)**:
   - Implement `internal/tui/view_stack.go` with unit tests for push, pop, top, and event dispatch.
   - Implement shared navigation traits (`internal/tui/navigation.go`).
   - Wrap the existing root chat interface as `ChatLayer`.
2. **Phase 2: Modal Dialog Migration**:
   - Migrate `AwaitingCompactConfirmation`, `AwaitingPruneConfirmation`, and `AwaitingToolConfirmation` into reusable `ConfirmOverlay` layers.
   - Retire the ad-hoc boolean confirmation flags.
3. **Phase 3: Screen Layer Migration**:
   - Migrate `ModeMap` into `MapLayer`.
   - Migrate `ModeMemories` into `MemoriesDeckLayer` and `MemoryCardOverlay`.
   - Retire `m.ViewMode` and legacy viewport override hacks.

---

## Consequences

### Positive

* **Zero Keystroke Leakage**: Guaranteed isolation ensures characters typed during navigation or modal interaction cannot bleed into the prompt buffer.
* **Radical State Simplification**: Eliminates 5 boolean flags, enum branches, and pointer checks from `Model`.
* **Clean Extensibility**: Adding the ADR-019 Config View becomes as simple as implementing `ViewLayer` and pushing it onto the stack.
* **Consistent Ergonomics**: Centralized Vim and arrow key navigation ensures uniform behavior across all lists and decks.

### Negative

* **Refactoring Surface Area**: Existing view rendering functions in `internal/tui/` must be adapted to conform to the `ViewLayer` interface.
