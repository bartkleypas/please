package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/storage"
)

// renderMemoriesView renders either the interactive memory card deck or the active card detail.
func (m *Model) renderMemoriesView() string {
	if m.MemoryDetailCard != nil {
		return m.renderMemoryCard(m.MemoryDetailCard)
	}
	return m.renderMemoriesDeck()
}

// renderMemoriesDeck renders the scrollable, selectable list of memory cards.
func (m *Model) renderMemoriesDeck() string {
	var sb strings.Builder

	// Header
	deckTitle := "🧠 Persistent Agent Memory Deck"
	if m.MemoryDeckFilter != "" {
		deckTitle = fmt.Sprintf("🧠 Memory Deck (Filter: %q)", m.MemoryDeckFilter)
	}
	sb.WriteString(memDeckHeaderStyle.Render(deckTitle))
	sb.WriteString("\n\n")

	if len(m.MemoryDeck) == 0 {
		if m.MemoryDeckFilter != "" {
			sb.WriteString(fmt.Sprintf("  No memories matching filter %q.\n", m.MemoryDeckFilter))
		} else {
			sb.WriteString("  No persistent memories in vault.\n  Memories are recorded autonomously when discovering workspace invariants or via `memory_store`.\n")
		}
		sb.WriteString("\n")
		sb.WriteString(memFooterStyle.Render("  [Esc: Return to Chat]"))
		return sb.String()
	}

	// Calculate dynamic key column width
	maxKeyLen := 20
	for _, card := range m.MemoryDeck {
		if len(card.Key) > maxKeyLen {
			maxKeyLen = len(card.Key)
		}
	}
	if maxKeyLen > 36 {
		maxKeyLen = 36
	}

	colFmt := fmt.Sprintf("%%-2s %%-%ds  %%-11s  %%-14s  %%-6s  %%-10s  %%s\n", maxKeyLen)
	rowFmt := fmt.Sprintf("%%-2s %%-%ds  %%-11s  %%-14s  %%-6d  %%-10s  %%s\n", maxKeyLen)

	dividerLen := maxKeyLen + 60
	if dividerLen > m.Width && m.Width > 0 {
		dividerLen = m.Width - 4
	}
	if dividerLen < 60 {
		dividerLen = 60
	}
	divider := strings.Repeat("─", dividerLen)

	sb.WriteString(fmt.Sprintf(colFmt, "", "KEY", "SCOPE", "CATEGORY", "ACCESS", "UPDATED", "CONTENT PREVIEW"))
	sb.WriteString(divider)
	sb.WriteString("\n")

	for i, card := range m.MemoryDeck {
		isSelected := i == m.MemoryDeckIndex
		cursor := "  "
		if isSelected {
			cursor = "➜ "
		}

		keyDisp := card.Key
		if len(keyDisp) > maxKeyLen {
			keyDisp = keyDisp[:maxKeyLen-2] + ".."
		}

		age := time.Since(card.UpdatedAt).Round(time.Minute).String()
		if age == "0s" {
			age = "just now"
		}

		contentClean := strings.ReplaceAll(card.Content, "\n", " ")
		maxContentLen := 35
		if len(contentClean) > maxContentLen {
			contentClean = contentClean[:maxContentLen-3] + "..."
		}

		if isSelected {
			rowStr := fmt.Sprintf(rowFmt,
				cursor,
				keyDisp,
				string(card.Scope),
				string(card.Category),
				card.AccessCount,
				age,
				contentClean,
			)
			sb.WriteString(memCursorRowStyle.Render(strings.TrimRight(rowStr, "\n")))
			sb.WriteString("\n")
		} else {
			sb.WriteString(fmt.Sprintf("%-2s %s  %s  %s  %-6d  %s  %s\n",
				cursor,
				memKeyStyle.Render(fmt.Sprintf(fmt.Sprintf("%%-%ds", maxKeyLen), keyDisp)),
				memScopeStyle.Render(fmt.Sprintf("%-11s", string(card.Scope))),
				memCatStyle.Render(fmt.Sprintf("%-14s", string(card.Category))),
				card.AccessCount,
				memDimStyle.Render(fmt.Sprintf("%-10s", age)),
				contentClean,
			))
		}
	}

	sb.WriteString(divider)
	sb.WriteString("\n")

	footer := fmt.Sprintf("  Card %d of %d  •  [↑/↓/j/k: Select | Enter: Flip Card | x/d: Prune | Esc: Return to Chat]", m.MemoryDeckIndex+1, len(m.MemoryDeck))
	sb.WriteString(memFooterStyle.Render(footer))

	return sb.String()
}

// renderMemoryCard renders the detailed expanded card view with bordered metadata and full content.
func (m *Model) renderMemoryCard(card *storage.Memory) string {
	var sb strings.Builder

	titleLine := fmt.Sprintf("🧠 Card: %s", card.Key)
	sb.WriteString(memKeyStyle.Render(titleLine))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("  • Scope:        %s\n", memScopeStyle.Render(string(card.Scope))))
	sb.WriteString(fmt.Sprintf("  • Category:     %s\n", memCatStyle.Render(string(card.Category))))
	sb.WriteString(fmt.Sprintf("  • Confidence:   %.2f\n", card.Confidence))
	if card.SessionID != "" {
		sb.WriteString(fmt.Sprintf("  • Session:      %s\n", card.SessionID))
	}
	if card.SourceNodeID != "" {
		sb.WriteString(fmt.Sprintf("  • Source Node:  %s\n", card.SourceNodeID))
	}
	if len(card.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("  • Tags:         %s\n", strings.Join(card.Tags, ", ")))
	}
	sb.WriteString(fmt.Sprintf("  • Access Count: %d\n", card.AccessCount))
	if card.LastAccessedAt != nil {
		sb.WriteString(fmt.Sprintf("  • Last Access:  %s (%s ago)\n", card.LastAccessedAt.Format("2006-01-02 15:04:05"), time.Since(*card.LastAccessedAt).Round(time.Minute)))
	}
	sb.WriteString(fmt.Sprintf("  • Created:      %s\n", card.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("  • Updated:      %s (%s ago)\n", card.UpdatedAt.Format("2006-01-02 15:04:05"), time.Since(card.UpdatedAt).Round(time.Minute)))

	sb.WriteString("\n────────────────────────────────────────────────────────────────────────────\n")
	sb.WriteString("Content:\n\n")
	sb.WriteString(card.Content)
	sb.WriteString("\n────────────────────────────────────────────────────────────────────────────\n\n")

	footer := "  [Esc/Backspace/q: Return to Deck | x/d: Prune Card]"
	sb.WriteString(memFooterStyle.Render(footer))

	return memCardBoxStyle.Render(sb.String())
}
