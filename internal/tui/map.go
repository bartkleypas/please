package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/charmbracelet/lipgloss"
)

// generateMapString builds the full visual tree of the graph
func (m *Model) generateMapString() string {
	var s strings.Builder
	title := "--- Narrative Graph Map ---"
	if m.SearchQuery != "" {
		title = fmt.Sprintf("--- Search Results: \"%s\" ---", m.SearchQuery)
	}
	s.WriteString(title + "\n\n")

	m.MapNodeIDs = nil // Clear IDs for fresh traversal

	roots := m.Manager.GetRoots()
	if len(roots) == 0 {
		s.WriteString("No nodes found in graph.\n")
	} else {
		// Use a cache for subtree activity to avoid redundant traversals
		activityCache := make(map[string]int64)

		// Sort roots by latest activity in their subtree (descending)
		sortedRoots := make([]*engine.Node, len(roots))
		copy(sortedRoots, roots)
		importSort(sortedRoots, activityCache, m)

		for i, root := range sortedRoots {
			isLast := i == len(sortedRoots)-1
			m.renderMap(&s, root.ID, "", isLast, activityCache)
		}
	}

	if len(m.MapNodeIDs) == 0 && m.SearchQuery != "" {
		s.WriteString("No nodes match your search query.\n")
	}

	return s.String()
}

func importSort(nodes []*engine.Node, cache map[string]int64, m *Model) {
	sort.Slice(nodes, func(i, j int) bool {
		return m.getLatestActivity(nodes[i].ID, cache) > m.getLatestActivity(nodes[j].ID, cache)
	})
}

func (m *Model) getLatestActivity(nodeID string, cache map[string]int64) int64 {
	if val, ok := cache[nodeID]; ok {
		return val
	}

	node, err := m.Manager.GetNode(nodeID)
	if err != nil {
		return 0
	}

	latest := node.Timestamp.UnixNano()
	children := m.Manager.GetChildren(node.ID)
	for _, child := range children {
		childActivity := m.getLatestActivity(child.ID, cache)
		if childActivity > latest {
			latest = childActivity
		}
	}

	cache[nodeID] = latest
	return latest
}

func (m *Model) renderMap(s *strings.Builder, nodeID string, indent string, isLast bool, activityCache map[string]int64) {
	node, err := m.Manager.GetNode(nodeID)
	if err != nil {
		return
	}

	// Filter out internal nodes unless we are searching or in audit mode
	if node.Internal && m.SearchQuery == "" && !m.AuditMode {
		// Even if we hide the node, we still want to visit its children
		children := m.Manager.GetChildren(node.ID)
		for i, child := range children {
			isLastChild := i == len(children)-1
			m.renderMap(s, child.ID, indent, isLastChild, activityCache)
		}
		return
	}

	// Filter by search query if present
	matchesSearch := true
	if m.SearchQuery != "" {
		matchesSearch = strings.Contains(strings.ToLower(node.Content), strings.ToLower(m.SearchQuery)) ||
			strings.Contains(strings.ToLower(node.ID), strings.ToLower(m.SearchQuery))
	}

	if matchesSearch {
		// Track rendered ID for navigation
		m.MapNodeIDs = append(m.MapNodeIDs, node.ID)

		treePrefix := "•"
		if indent != "" {
			if isLast {
				treePrefix = "└"
			} else {
				treePrefix = "├"
			}
		}

		encIndicator := ""
		if node.Encrypted {
			encIndicator = markStyle.Render("🔒")
		}

		bookmark := ""
		if node.Metadata != nil && node.Metadata["bookmarked"] == "true" {
			bookmark = markStyle.Render("⭐")
		}

		// Tool call indicator
		toolIndicator := ""
		if len(node.ToolCalls) > 0 {
			toolIndicator = markStyle.Render("🛠️")
		} else if node.Role == engine.RoleTool {
			toolIndicator = markStyle.Render("⚙️")
		}

		// Current location marker moved here
		locationMarker := ""
		if node.ID == m.CurrentID {
			locationMarker = markStyle.Render("📍")
		}

		shortID := node.ID
		if !m.AuditMode && len(shortID) > 8 {
			shortID = shortID[len(shortID)-8:]
		}

		idStr := "[" + shortID + "]"
		// Apply highlight if selected
		if m.ViewMode == ModeMap && len(m.MapNodeIDs)-1 == m.MapSelectionIndex {
			idStr = highlightStyle.Render(idStr)
		}

		// Folded Indicator
		foldedIndicator := ""
		children := m.Manager.GetChildren(node.ID)
		if len(children) > 0 {
			if m.CollapsedNodes[node.ID] {
				foldedIndicator = " " + markStyle.Render(fmt.Sprintf("[+%d]", len(children)))
			}
		}

		// Pulse Effect Logic
		age := time.Since(node.Timestamp)
		var roleStyle lipgloss.Style

		if age < 500*time.Millisecond {
			// Phase 1: Immediate Growth (Luminous Mint)
			roleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true)
		} else if age < 2*time.Second {
			// Phase 2: Settling (Muted Green)
			roleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#2d5a45"))
		} else {
			// Phase 3: Resting (Standard Theme)
			roleStyle = getRoleStyle(node.Role)
		}

		preview := node.Content
		if len(preview) > 30 {
			preview = preview[:27] + "..."
		}
		preview = strings.ReplaceAll(preview, "\n", " ")

		fmt.Fprintf(s, " %s%s%s%s%s%s%s %s:%s%s\n", indent, treePrefix, idStr, encIndicator, bookmark, toolIndicator, locationMarker, roleStyle.Render(string(node.Role)), preview, foldedIndicator)
	}

	// If collapsed, don't render children
	if m.CollapsedNodes[nodeID] {
		return
	}

	children := m.Manager.GetChildren(node.ID)
	// Sort children by latest activity in their subtree (descending)
	sortedChildren := make([]*engine.Node, len(children))
	copy(sortedChildren, children)
	importSort(sortedChildren, activityCache, m)

	for i, child := range sortedChildren {
		childIsLast := i == len(sortedChildren)-1
		childIndent := indent
		if indent == "" {
			childIndent = " "
		} else if isLast {
			childIndent += " "
		} else {
			childIndent += "│"
		}
		m.renderMap(s, child.ID, childIndent, childIsLast, activityCache)
	}
}
