# Project: Contain the Horizontal Drift

## 1. Problem Statement
The web-based DAG visualization currently suffers from unbounded horizontal expansion. As the depth of the conversation tree increases, the cumulative left margin applied to descendant nodes pushes the content progressively to the right, eventually rendering the "leaf" nodes inaccessible without significant horizontal scrolling.

## 2. Root Cause
The CSS rule for '.node' (or the logic generating the nested structure) applies a fixed 'margin-left' (e.g., 24px) for every level of nesting. Because the DOM structure is hierarchical, this margin accumulates linearly with depth:
- Depth 0: 0px
- Depth 1: 24px
- Depth 2: 48px
- ...
- Depth N: N * 24px

This causes the 'container' to expand horizontally beyond the viewport/container limits.

## 3. Proposed Solutions

### Strategy Alpha: The Margin Cap (Low Complexity)
Limit the cumulative margin to a maximum value (e.g., 120px). Once a node's depth exceeds this threshold, the margin stops increasing.
- **Pros:** Extremely easy to implement via CSS.
- **Cons:** At very high depths, the visual distinction between levels 10 and 11 becomes solely dependent on other markers.

### Strategy Beta: The Vertical Guide (Medium Complexity)
Cap the margin at a reasonable level (e.g., 120px) and introduce a vertical "thread" or border-left on the parent nodes to visually indicate the lineage of deeply nested nodes.
- **Pros:** Maintains visual hierarchy without horizontal drift.
- **Cons:** Requires minor changes to both CSS and potentially the JS rendering logic.

### Strategy Gamma: The Flat-List Refactor (High Complexity)
Change the rendering logic from a nested DOM structure to a flat list of nodes where indentation is a property of the node's data, not a result of DOM nesting.
- **Pros:** Most robust; solves the problem at the architectural level.
- **Cons:** Requires a significant rewrite of the D3 rendering logic and the HTML structure.

## 4. Implementation Plan (Targeting Strategy Alpha/Beta)
1.  **Identify** the specific CSS selector in  responsible for the .
2.  **Apply** a  to the  element to ensure text wrapping.
3.  **Implement** a  cap using either a  on the container or a CSS rule that limits the accumulation of margin.
4.  **Verify** that text within the nodes still wraps correctly using .
