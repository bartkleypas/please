# Package internal/tui Index

The `internal/tui` package implements the Terminal User Interface for `Please`, powered by Charm's **Bubble Tea** and **Lipgloss** ecosystems. It provides a visual chat room, an interactive DAG tree visualizer, and Vim-style navigation.

## Core Files & Components

### 1. State Machine & Event Loop
*   [tui.go](tui.go) - Initializer for the TUI runtime environment.
*   [model.go](model.go) - Declares the core Bubble Tea `Model` state struct tracking active views, inputs, options, and stream buffers.
*   [handlers.go](handlers.go) - Routes input events (key presses, window resizes) to specific sub-systems.
*   [tea_cmds.go](tea_cmds.go) - Declares asynchronous commands (e.g., triggering background stream reads, file storage actions) that update the UI loop.

### 2. View Rendering
*   [view.go](view.go) - Central layout manager that checks states and renders the chat panel, DAG map overlay, or diagnostic HUD.
*   [styles.go](styles.go) - Declares the Lipgloss colors, margins, padding, and custom boundaries.

### 3. Interactive TUI Views
*   [chat.go](chat.go) & [messages.go](messages.go) - Manages and renders the multi-turn chat dialogues, user inputs, and markdown outputs.
*   [map.go](map.go) - Renders the text-based DAG tree-map visualizer showing conversation branching and node IDs.

### 4. Navigation & Shortcuts
*   [keys.go](keys.go) - Defines keybindings and Vim navigation controls (`h/j/k/l` for graph traversal, `c` for compaction, `d` for deletion).
*   [commands.go](commands.go) - Implementation logic for TUI slash commands (such as `/persona`, `/pacing`, `/jump`, `/gc`).

### 5. Stream Processing & Tools
*   [streaming.go](streaming.go) - Implements token buffering and punctuation-sensitive timers for natural reading pace streaming.
*   [tools_handlers.go](tools_handlers.go) - Manages TUI confirmation popups, styling, and output feedback for tool calls.
