package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/engine"
)

func (m *Model) renderNode(node *engine.Node) string {
	roleStyle := getRoleStyle(node.Role)

	prefix := string(node.Role)
	if node.Internal {
		prefix = "INTERNAL " + prefix
	}
	if m.AuditMode {
		prefix = fmt.Sprintf("%s (%s)", prefix, node.ID)
	}
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}

	var s strings.Builder
	s.WriteString(roleStyle.Render(prefix) + ":\n")

	// 1. Render Thought (Lane A)
	if node.Thought != "" {
		s.WriteString(thoughtStyle.Render(wrapText(node.Thought, wrapWidth)) + "\n")
	}

	// 2. Render Tool Interleaving (Lanes B & C)
	for i, call := range node.ToolCalls {
		// Announce Action (Lane B)
		s.WriteString(markStyle.Render(fmt.Sprintf("⚒️  Executing %s(%s)...", call.Function.Name, string(call.Function.Arguments))) + "\n")

		// Render Observation if available (Lane C)
		if i < len(node.Observations) {
			obs := node.Observations[i]
			summary := obs.Result
			if len(summary) > 200 {
				summary = summary[:200] + "... (truncated)"
			}
			s.WriteString(helpStyle.Render(fmt.Sprintf("  ✅ Result: %s", summary)) + "\n")
		}
	}

	// 3. Render Final Response (Lane D)
	if node.Content != "" {
		s.WriteString(wrapText(node.Content, wrapWidth) + "\n")
	}

	return s.String()
}

func (m *Model) updateViewportWithNode(node *engine.Node) {
	line := m.renderNode(node)
	m.ChatHistoryBuffer += line
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
	m.LastActivity = time.Now()
}

func (m *Model) updateViewportWithStreaming() {
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}

	var s strings.Builder
	s.WriteString(botStyle.Render(string(engine.RoleAssistant)) + ":\n")

	if m.CurrentStreamingThought != "" {
		s.WriteString(thoughtStyle.Render(wrapText(m.CurrentStreamingThought, wrapWidth)) + "\n")
	}

	// During streaming, we might have interleaved observations from a previous pause
	if m.InterleavingNodeID != "" {
		node, err := m.Manager.GetNode(m.InterleavingNodeID)
		if err == nil {
			for i, call := range node.ToolCalls {
				s.WriteString(markStyle.Render(fmt.Sprintf("⚒️  Executing %s...", call.Function.Name)) + "\n")
				if i < len(node.Observations) {
					s.WriteString(helpStyle.Render("  ✅ Observation received.") + "\n")
				}
			}
		}
	}

	if m.CurrentStreamingContent != "" {
		s.WriteString(wrapText(m.CurrentStreamingContent, wrapWidth) + "\n")
	}

	m.Viewport.SetContent(m.ChatHistoryBuffer + s.String())
	m.Viewport.GotoBottom()
}

func (m *Model) syncMapSelection() {
	m.ViewportOverride = m.generateMapString()
	m.Viewport.SetContent(m.ViewportOverride)

	// Ensure selection is within bounds (important after pruning)
	if m.MapSelectionIndex >= len(m.MapNodeIDs) {
		m.MapSelectionIndex = len(m.MapNodeIDs) - 1
	}
	if m.MapSelectionIndex < 0 {
		m.MapSelectionIndex = 0
	}

	// Ensure selected node is in view
	if len(m.MapNodeIDs) > 0 {
		if m.MapSelectionIndex < m.Viewport.YOffset {
			m.Viewport.SetYOffset(m.MapSelectionIndex)
		} else if m.MapSelectionIndex >= m.Viewport.YOffset+m.Viewport.Height-2 {
			m.Viewport.SetYOffset(m.MapSelectionIndex - m.Viewport.Height + 3)
		}
	}
}

func (m *Model) updateViewportWithJump() {
	m.updateViewportContent()
}

func (m *Model) updateViewportContent() {
	m.ViewportOverride = "" // Clear override when refreshing chat history
	var s strings.Builder

	path, err := m.Manager.GetPath(m.CurrentID)
	if err != nil || len(path) == 0 {
		s.WriteString("Welcome to Please. Start typing to begin the story...\n")
	} else {
		for _, node := range path {
			if node.Internal && !m.AuditMode {
				continue
			}
			s.WriteString(m.renderNode(node))
		}
	}

	m.ChatHistoryBuffer = s.String()
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
}
