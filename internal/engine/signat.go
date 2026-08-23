package engine

import (
	"strings"
)

// isEmojiRune returns true if the rune belongs to standard Unicode emoji ranges
func isEmojiRune(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1FAFF) || // Misc Symbols & Pictographs, Emoticons, Transport, etc.
		(r >= 0x2600 && r <= 0x27BF) || // Misc Symbols, Dingbats
		(r >= 0x1F600 && r <= 0x1F64F) || // Emoticons
		(r >= 0x1F680 && r <= 0x1F6FF) || // Transport and Map
		(r >= 0x2B50 && r <= 0x2B55) || // Stars, circles
		r == 0xFE0F || r == 0x200D || // Variation selector, Zero-Width Joiner
		(r >= 0x1F900 && r <= 0x1F9FF) || // Supplemental Symbols
		(r >= 0x1FA70 && r <= 0x1FAFF) // Extended-A symbols
}

// ExtractSignat detects trailing 1-3 emoji semantic signatures (or <signat> tags) from assistant turns.
// It returns the cleaned content and the extracted signat string.
func ExtractSignat(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text, ""
	}

	// 1. Check for explicit <signat>...</signat>
	startTag := "<signat>"
	endTag := "</signat>"
	if sIdx := strings.LastIndex(trimmed, startTag); sIdx != -1 {
		if eIdx := strings.LastIndex(trimmed, endTag); eIdx > sIdx {
			signat := strings.TrimSpace(trimmed[sIdx+len(startTag) : eIdx])
			cleaned := strings.TrimSpace(trimmed[:sIdx] + trimmed[eIdx+len(endTag):])
			return cleaned, signat
		}
	}

	// 2. Check for trailing emoji sequence
	runes := []rune(trimmed)
	if len(runes) == 0 {
		return text, ""
	}

	end := len(runes)
	start := end
	for start > 0 {
		r := runes[start-1]
		if isEmojiRune(r) || r == ' ' || r == '\n' || r == '\t' {
			start--
		} else {
			break
		}
	}

	candidate := strings.TrimSpace(string(runes[start:end]))
	if candidate == "" {
		return text, ""
	}

	candRunes := []rune(candidate)
	hasEmoji := false
	emojiCount := 0
	for _, r := range candRunes {
		if isEmojiRune(r) && r != 0xFE0F && r != 0x200D {
			hasEmoji = true
			emojiCount++
		}
	}

	// Valid signat is 1-4 emojis max
	if hasEmoji && emojiCount <= 4 && len(candRunes) <= 16 {
		cleaned := strings.TrimRight(string(runes[:start]), " \t\n\r")
		return cleaned, candidate
	}

	return text, ""
}
