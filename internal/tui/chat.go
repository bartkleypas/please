package tui

import (
	"fmt"
	"strings"
	"time"

	"org.kleypas.please/internal/engine"
)

func (m *Model) renderNode(node *engine.Node) string {
	roleStyle := getRoleStyle(node.Role)

	prefix := string(node.Role)
	wrapWidth := m.Width - 4
	if wrapWidth <= 0 {
		wrapWidth = 80
	}

	content := node.Content
	if node.Role == engine.RoleAssistant && len(node.ToolCalls) > 0 {
		var toolStrings []string
		for _, tc := range node.ToolCalls {
			toolStrings = append(toolStrings, fmt.Sprintf("[Tool Call: %s(%s)]", tc.Function.Name, string(tc.Function.Arguments)))
		}
		if content != "" {
			content += "\n"
		}
		content += strings.Join(toolStrings, "\n")
	} else if node.Role == engine.RoleTool {
		content = fmt.Sprintf("[Tool Result (%s)]: %s", node.ToolCallID, content)
	}

	wrappedContent := wrapText(content, wrapWidth)
	return fmt.Sprintf("%s:\n%s\n", roleStyle.Render(prefix), wrappedContent)
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
	cleanContent := ScrubThought(m.CurrentStreamingContent)
	wrappedContent := wrapText(cleanContent, wrapWidth)

	line := fmt.Sprintf("%s:\n%s\n", botStyle.Render(string(engine.RoleAssistant)), wrappedContent)
	m.Viewport.SetContent(m.ChatHistoryBuffer + line)
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
			s.WriteString(m.renderNode(node))
		}
	}

	m.ChatHistoryBuffer = s.String()
	m.Viewport.SetContent(m.ChatHistoryBuffer)
	m.Viewport.GotoBottom()
}
