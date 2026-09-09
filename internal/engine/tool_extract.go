package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var (
	// Matches Gemma-style <call>tool_name{...}</call>
	gemmaCallRegex = regexp.MustCompile(`(?s)<call>([a-zA-Z0-9_-]+)\s*\{(.*?)\}</call>`)

	// Matches tool_code: tool_name{...}
	toolCodeRegex = regexp.MustCompile(`(?m)^tool_code:\s*([a-zA-Z0-9_-]+)\s*\{(.*?)\}\s*(.*?)$`)

	// Matches <tool_call>...</tool_call> or <|tool_call|>...</|tool_call|> or <tool_call|>...</tool_call|>
	toolCallTagRegex = regexp.MustCompile(`(?s)<(?:\|tool_call\||tool_call\||tool_call)>\s*(.*?)\s*(?:</(?:\|tool_call\||tool_call\||tool_call)>|$)`)

	// Matches orphan delimiter tokens like <tool_call|>, <|tool_call|>, <tool_call>, </tool_call>
	orphanToolTokenRegex = regexp.MustCompile(`(?i)</?(?:\|tool_call\||tool_call\||tool_call)>`)

	// Regex to quote unquoted object keys in pseudo-JSON (e.g. {path: "internal"} -> {"path": "internal"})
	unquotedKeyRegex = regexp.MustCompile(`([{,]\s*)([a-zA-Z0-9_]+)\s*:`)
)

// normalizePseudoJSON converts model pseudo-JSON (like Gemma's <|"|> quotes and unquoted keys) into valid JSON
func normalizePseudoJSON(rawArgs string) string {
	s := strings.TrimSpace(rawArgs)
	// Replace Gemma's string quotation tokens
	s = strings.ReplaceAll(s, `<|"|>`, `"`)

	if !strings.HasPrefix(s, "{") {
		s = "{" + s
	}
	if !strings.HasSuffix(s, "}") {
		s = s + "}"
	}

	// Quote any unquoted keys
	s = unquotedKeyRegex.ReplaceAllString(s, `$1"$2":`)
	return s
}

// ExtractContentToolCalls detects raw tool call patterns leaked into text content
// when model providers or local inference backends fail to intercept them into structured API tool calls.
// It returns the cleaned text content and any extracted ToolCall items.
func ExtractContentToolCalls(content string) (string, []ToolCall) {
	var toolCalls []ToolCall
	cleaned := content

	// 1. Check for Gemma <call>tool_name{...}</call>
	if matches := gemmaCallRegex.FindAllStringSubmatchIndex(content, -1); len(matches) > 0 {
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			toolName := content[m[2]:m[3]]
			rawArgs := content[m[4]:m[5]]

			normJSON := normalizePseudoJSON(rawArgs)
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(normJSON), &parsed); err == nil {
				jsonBytes, _ := json.Marshal(parsed)
				callID := fmt.Sprintf("call_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
				toolCalls = append([]ToolCall{{
					ID:   callID,
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      toolName,
						Arguments: jsonBytes,
					},
				}}, toolCalls...)

				cleaned = strings.TrimSpace(cleaned[:m[0]] + cleaned[m[1]:])
			}
		}
		if len(toolCalls) > 0 {
			return cleaned, toolCalls
		}
	}

	// 2. Check for tool_code: tool_name{...}
	if matches := toolCodeRegex.FindAllStringSubmatchIndex(content, -1); len(matches) > 0 {
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			toolName := content[m[2]:m[3]]
			rawArgs := content[m[4]:m[5]]

			normJSON := normalizePseudoJSON(rawArgs)
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(normJSON), &parsed); err == nil {
				jsonBytes, _ := json.Marshal(parsed)
				callID := fmt.Sprintf("call_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
				toolCalls = append([]ToolCall{{
					ID:   callID,
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      toolName,
						Arguments: jsonBytes,
					},
				}}, toolCalls...)

				cleaned = strings.TrimSpace(cleaned[:m[0]] + cleaned[m[1]:])
			}
		}
		if len(toolCalls) > 0 {
			return cleaned, toolCalls
		}
	}

	// 3. Check for <tool_call>...</tool_call> tags (JSON or tool_name{...} format)
	if matches := toolCallTagRegex.FindAllStringSubmatchIndex(content, -1); len(matches) > 0 {
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			rawBody := strings.TrimSpace(content[m[2]:m[3]])
			if rawBody == "" {
				cleaned = strings.TrimSpace(cleaned[:m[0]] + cleaned[m[1]:])
				continue
			}

			// Try 3a: JSON format {"name": "...", "arguments": ...}
			var jsonMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawBody), &jsonMap); err == nil {
				toolName := ""
				var argsBytes []byte
				if n, ok := jsonMap["name"].(string); ok && n != "" {
					toolName = n
					if rawArg, ok := jsonMap["arguments"]; ok {
						if strArg, ok := rawArg.(string); ok {
							argsBytes = []byte(strArg)
						} else {
							argsBytes, _ = json.Marshal(rawArg)
						}
					}
				} else if f, ok := jsonMap["function"].(map[string]interface{}); ok {
					if n, ok := f["name"].(string); ok && n != "" {
						toolName = n
						if rawArg, ok := f["arguments"]; ok {
							if strArg, ok := rawArg.(string); ok {
								argsBytes = []byte(strArg)
							} else {
								argsBytes, _ = json.Marshal(rawArg)
							}
						}
					}
				}

				if toolName != "" {
					if len(argsBytes) == 0 {
						argsBytes = []byte("{}")
					}
					callID := fmt.Sprintf("call_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
					toolCalls = append([]ToolCall{{
						ID:   callID,
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      toolName,
							Arguments: argsBytes,
						},
					}}, toolCalls...)
					cleaned = strings.TrimSpace(cleaned[:m[0]] + cleaned[m[1]:])
					continue
				}
			}

			// Try 3b: function syntax inside <tool_call>: [call:]tool_name{...}
			funcRegex := regexp.MustCompile(`^(?:call:)?([a-zA-Z0-9_-]+)\s*\{(.*?)\}`)
			if fMatch := funcRegex.FindStringSubmatch(rawBody); len(fMatch) > 0 {
				toolName := fMatch[1]
				normJSON := normalizePseudoJSON(fMatch[2])
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(normJSON), &parsed); err == nil {
					jsonBytes, _ := json.Marshal(parsed)
					callID := fmt.Sprintf("call_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
					toolCalls = append([]ToolCall{{
						ID:   callID,
						Type: "function",
						Function: struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						}{
							Name:      toolName,
							Arguments: jsonBytes,
						},
					}}, toolCalls...)
					cleaned = strings.TrimSpace(cleaned[:m[0]] + cleaned[m[1]:])
					continue
				}
			}
		}
		if len(toolCalls) > 0 {
			cleaned = strings.TrimSpace(orphanToolTokenRegex.ReplaceAllString(cleaned, ""))
			return cleaned, toolCalls
		}
	}

	// 4. Strip any leaked orphan delimiter tokens (e.g. <tool_call|>, <|tool_call|>) from content
	cleaned = strings.TrimSpace(orphanToolTokenRegex.ReplaceAllString(cleaned, ""))
	return cleaned, nil
}
