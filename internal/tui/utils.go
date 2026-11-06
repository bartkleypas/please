package tui

import (
	"strings"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/charmbracelet/lipgloss"
)

func getRoleStyle(role domain.Role) lipgloss.Style {
	switch role {
	case domain.RoleAssistant:
		return botStyle
	case domain.RoleTool:
		return markStyle
	case domain.RoleSystem:
		return titleStyle
	default:
		return userStyle
	}
}

// wrapText is a helper to manually insert newlines into long strings
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	var wrapped strings.Builder

	for i, line := range lines {
		if line == "" {
			wrapped.WriteString("\n")
			continue
		}

		// 1. Capture leading whitespace
		trimmed := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(trimmed)]

		// 2. Wrap the trimmed content
		words := strings.Fields(trimmed)
		if len(words) == 0 {
			wrapped.WriteString(line)
			if i < len(lines)-1 {
				wrapped.WriteString("\n")
			}
			continue
		}

		var lineBuilder strings.Builder
		lineBuilder.WriteString(indent)
		currentLineLength := len(indent)

		for _, word := range words {
			wordLen := len(word)
			if currentLineLength > len(indent) && currentLineLength+wordLen+1 > width {
				lineBuilder.WriteString("\n")
				lineBuilder.WriteString(indent)
				currentLineLength = len(indent)
			} else if currentLineLength > len(indent) {
				lineBuilder.WriteString(" ")
				currentLineLength++
			}
			lineBuilder.WriteString(word)
			currentLineLength += wordLen
		}

		wrapped.WriteString(lineBuilder.String())

		if i < len(lines)-1 {
			wrapped.WriteString("\n")
		}
	}

	return wrapped.String()
}
