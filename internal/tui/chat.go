package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/engine"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
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
	s.WriteString(roleStyle.Render(prefix))
	s.WriteString(":\n")

	if len(node.Images) > 0 {
		var filenames []string
		for _, img := range node.Images {
			filenames = append(filenames, filepath.Base(img))
		}
		var sdSummary string
		for _, imgPath := range node.Images {
			file, err := os.Open(imgPath)
			if err == nil {
				_, format, decodeErr := image.DecodeConfig(file)
				if decodeErr == nil && strings.ToLower(format) == "png" {
					_, _ = file.Seek(0, 0)
					meta, err := engine.ExtractPNGMetadata(file)
					if err == nil {
						if params, exists := meta["parameters"]; exists {
							sdDetails := engine.ParseSDParameters(params)
							if prompt, ok := sdDetails["sd_prompt"]; ok && prompt != "" {
								shortPrompt := prompt
								if len(shortPrompt) > 40 {
									shortPrompt = shortPrompt[:37] + "..."
								}
								sdSummary = fmt.Sprintf(" (SD: \"%s\")", shortPrompt)
								file.Close()
								break
							}
						}
					}
				}
				file.Close()
			}
		}
		s.WriteString(helpStyle.Render(fmt.Sprintf("  🖼️  [Images: %s]%s", strings.Join(filenames, ", "), sdSummary)))
		s.WriteString("\n")
	}

	// 1. Check for segments in metadata for chronological rendering
	var segments []struct {
		Content string `json:"content"`
		Thought string `json:"thought"`
	}
	if node.Role == engine.RoleAssistant && node.Metadata != nil && node.Metadata["segments"] != "" {
		_ = json.Unmarshal([]byte(node.Metadata["segments"]), &segments)
	}

	expanded := m.isThoughtExpanded(node.ID)

	if len(segments) > 0 {
		var totalThoughtChars int
		for _, seg := range segments {
			totalThoughtChars += len(seg.Thought)
		}

		if totalThoughtChars > 0 {
			if !expanded {
				s.WriteString(thoughtBadgeStyle.Render(fmt.Sprintf("▶ Thought Process (%d chars) • [Tab to expand]", totalThoughtChars)))
				s.WriteString("\n")
			} else {
				s.WriteString(thoughtBadgeStyle.Render(fmt.Sprintf("▼ Thought Process (%d chars) • [Tab to collapse]", totalThoughtChars)))
				s.WriteString("\n")
			}
		}

		for j, seg := range segments {
			if seg.Thought != "" && expanded {
				s.WriteString(thoughtStyle.Render(wrapText(seg.Thought, wrapWidth)))
				s.WriteString("\n")
			}
			if j < len(node.ToolCalls) {
				call := node.ToolCalls[j]
				s.WriteString(markStyle.Render(fmt.Sprintf("⚒️  Executing %s(%s)...", call.Function.Name, string(call.Function.Arguments))))
				s.WriteString("\n")

				if j < len(node.Observations) {
					obs := node.Observations[j]
					summary := obs.Result
					if len(summary) > 200 {
						summary = summary[:200] + "... (truncated)"
					}
					s.WriteString(helpStyle.Render(fmt.Sprintf("  ✅ Result: %s", summary)))
					s.WriteString("\n")
				}
			}
			if seg.Content != "" {
				s.WriteString(wrapText(seg.Content, wrapWidth))
				s.WriteString("\n")
			}
		}
	} else {
		// Fallback to non-segmented rendering
		// 1. Render Thought (Lane A)
		if node.Thought != "" {
			if !expanded {
				s.WriteString(thoughtBadgeStyle.Render(fmt.Sprintf("▶ Thought Process (%d chars) • [Tab to expand]", len(node.Thought))))
				s.WriteString("\n")
			} else {
				s.WriteString(thoughtBadgeStyle.Render(fmt.Sprintf("▼ Thought Process (%d chars) • [Tab to collapse]", len(node.Thought))))
				s.WriteString("\n")
				s.WriteString(thoughtStyle.Render(wrapText(node.Thought, wrapWidth)))
				s.WriteString("\n")
			}
		}

		// 2. Render Tool Interleaving (Lanes B & C)
		for i, call := range node.ToolCalls {
			// Announce Action (Lane B)
			s.WriteString(markStyle.Render(fmt.Sprintf("⚒️  Executing %s(%s)...", call.Function.Name, string(call.Function.Arguments))))
			s.WriteString("\n")

			// Render Observation if available (Lane C)
			if i < len(node.Observations) {
				obs := node.Observations[i]
				summary := obs.Result
				if len(summary) > 200 {
					summary = summary[:200] + "... (truncated)"
				}
				s.WriteString(helpStyle.Render(fmt.Sprintf("  ✅ Result: %s", summary)))
				s.WriteString("\n")
			}
		}

		// 3. Render Final Response (Lane D)
		if node.Content != "" {
			s.WriteString(wrapText(node.Content, wrapWidth))
			s.WriteString("\n")
		}
	}

	return s.String()
}

func (m *Model) isThoughtExpanded(nodeID string) bool {
	if m.ExpandedThoughts == nil {
		return !m.DefaultFoldThoughts
	}
	if expanded, ok := m.ExpandedThoughts[nodeID]; ok {
		return expanded
	}
	return !m.DefaultFoldThoughts
}

func (m *Model) updateViewportWithNode(node *engine.Node) {
	m.ViewportOverride = ""
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
	s.WriteString(botStyle.Render(string(engine.RoleAssistant)))
	s.WriteString(":\n")

	// Render already committed segments during a resumed streaming session
	if m.InterleavingNodeID != "" {
		node, err := m.Manager.GetNode(m.InterleavingNodeID)
		if err == nil {
			var segments []struct {
				Content string `json:"content"`
				Thought string `json:"thought"`
			}
			if node.Metadata != nil && node.Metadata["segments"] != "" {
				_ = json.Unmarshal([]byte(node.Metadata["segments"]), &segments)
			}

			if len(segments) > 0 {
				for j, seg := range segments {
					if seg.Thought != "" {
						s.WriteString(thoughtStyle.Render(wrapText(seg.Thought, wrapWidth)))
						s.WriteString("\n")
					}
					if j < len(node.ToolCalls) {
						call := node.ToolCalls[j]
						s.WriteString(markStyle.Render(fmt.Sprintf("⚒️  Executing %s...", call.Function.Name)))
						s.WriteString("\n")
						if j < len(node.Observations) {
							s.WriteString(helpStyle.Render("  ✅ Observation received."))
							s.WriteString("\n")
						}
					}
					if seg.Content != "" {
						s.WriteString(wrapText(seg.Content, wrapWidth))
						s.WriteString("\n")
					}
				}
			} else {
				// Fallback if no segments metadata
				for i, call := range node.ToolCalls {
					s.WriteString(markStyle.Render(fmt.Sprintf("⚒️  Executing %s...", call.Function.Name)))
					s.WriteString("\n")
					if i < len(node.Observations) {
						s.WriteString(helpStyle.Render("  ✅ Observation received."))
						s.WriteString("\n")
					}
				}
				if node.Content != "" {
					s.WriteString(wrapText(node.Content, wrapWidth))
					s.WriteString("\n")
				}
			}
		}
	}

	if m.CurrentStreamingThought != "" {
		s.WriteString(thoughtStyle.Render(wrapText(m.CurrentStreamingThought, wrapWidth)))
		s.WriteString("\n")
	}

	if m.CurrentStreamingContent != "" {
		s.WriteString(wrapText(m.CurrentStreamingContent, wrapWidth))
		s.WriteString("\n")
	}

	m.Viewport.SetContent(m.ChatHistoryBuffer + s.String())
	m.Viewport.GotoBottom()
}

func (m *Model) syncMapSelection() {
	var selectedID string
	if m.MapSelectionIndex >= 0 && m.MapSelectionIndex < len(m.MapNodeIDs) {
		selectedID = m.MapNodeIDs[m.MapSelectionIndex]
	}

	m.ViewportOverride = m.generateMapString()
	m.Viewport.SetContent(m.ViewportOverride)

	// Re-locate selected node in updated map slice to prevent cursor jumping
	if selectedID != "" {
		found := false
		for i, id := range m.MapNodeIDs {
			if id == selectedID {
				m.MapSelectionIndex = i
				found = true
				break
			}
		}
		if !found {
			// If node was pruned or removed, clamp gracefully
			if m.MapSelectionIndex >= len(m.MapNodeIDs) {
				m.MapSelectionIndex = len(m.MapNodeIDs) - 1
			}
		}
	} else {
		if m.MapSelectionIndex >= len(m.MapNodeIDs) {
			m.MapSelectionIndex = len(m.MapNodeIDs) - 1
		}
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

// navigateToNode handles jumping/selecting a specific node in the DAG.
// If the selected node is a User turn, it rewinds the active anchor to the
// node's ParentID (the preceding assistant turn or system prompt), pre-populates
// the input box with the user turn's content for editing, and restores any images.
// If the selected node is an Assistant/System/Tool turn, it sets the active anchor
// directly to that node and clears the input box for a fresh prompt.
func (m *Model) navigateToNode(node *engine.Node) {
	if node == nil {
		return
	}
	if node.Role == engine.RoleUser {
		m.CurrentID = node.ParentID
		m.TextInput.SetValue(node.Content)
		m.TextInput.CursorEnd()
		if len(node.Images) > 0 {
			m.PendingImages = append([]string(nil), node.Images...)
		} else {
			m.PendingImages = nil
		}
		m.Notification = "Rewound to edit user turn."
	} else {
		m.CurrentID = node.ID
		m.TextInput.Reset()
		m.PendingImages = nil
		m.Notification = fmt.Sprintf("Jumped to %s", node.ID)
	}
	m.ViewMode = ModeChat
	m.ViewportOverride = ""
	m.updateViewportContent()
}

func (m *Model) updateViewportContent() {
	m.updateViewportContentPreservingOffset(-1)
}

func (m *Model) updateViewportContentPreservingOffset(savedOffset int) {
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
	if savedOffset >= 0 {
		m.Viewport.SetYOffset(savedOffset)
	} else {
		m.Viewport.GotoBottom()
	}
}
