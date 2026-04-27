package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"org.kleypas.please/internal/engine"
)

func getRoleStyle(role engine.Role) lipgloss.Style {
	switch role {
	case engine.RoleAssistant:
		return botStyle
	case engine.RoleTool:
		return markStyle
	case engine.RoleSystem:
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
		var lineBuilder strings.Builder
		currentLineLength := 0

		for _, word := range words {
			wordLen := len(word)
			if currentLineLength+wordLen+1 > width {
				lineBuilder.WriteString("\n")
				currentLineLength = 0
			} else if currentLineLength > 0 {
				lineBuilder.WriteString(" ")
				currentLineLength++
			}
			lineBuilder.WriteString(word)
			currentLineLength += wordLen
		}

		// 3. Prepend the original indentation to the first line of the wrapped result
		wrapped.WriteString(indent + lineBuilder.String())

		if i < len(lines)-1 {
			wrapped.WriteString("\n")
		}
	}

	return wrapped.String()
}
