# DAG Visualization Enhancements
*Curated by the Librarian (George)*

This document outlines the proposed "visual sprucing" for the DAG representation within the `Please` TUI. The goal is to move beyond static text and provide a more dynamic, intuitive sense of growth and flow.

## The Pulse (Temporal Visual Cues)
**Objective:** Provide immediate visual feedback when the DAG mutates.

- **Concept:** New nodes should "flash" upon creation before settling into their resting state.
- **Implementation:**
    - Assign a `CreatedAt` timestamp or a `FrameCount` to new nodes.
    - In the Bubble Tea `View` function, check the age of the node.
    - **Phase 1 (Immediate):** Render with `lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold()` (Luminous Mint).
    - **Phase 2 (Decay):** Transition to a muted green or `Dim` over several ticks.
    - **Phase 3 (Resting):** Return to the standard forest theme color.
- **User Value:** Prevents the "did it actually update?" confusion by highlighting the exact point of growth.
