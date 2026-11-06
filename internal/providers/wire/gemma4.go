package wire

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/google/uuid"
)

// Gemma 4 Wire Grammar Tokens
const (
	TagTurnOpen          = "<|turn>"
	TagTurnClose         = "<turn|>"
	TagThink             = "<|think|>"
	TagToolOpen          = "<|tool>"
	TagToolClose         = "<tool|>"
	TagChannelOpen       = "<|channel>"
	TagChannelClose      = "<channel|>"
	TagToolCallOpen      = "<|tool_call>"
	TagToolCallClose     = "<tool_call|>"
	TagToolResponseOpen  = "<|tool_response>"
	TagToolResponseClose = "<tool_response|>"
	TagStringDelimiter   = "<|\"|>"
)

// WireGrammar contracts tailored to reasoning effort levels ("off", "low", "medium", "high").
// Note: Do NOT embed raw special control tokens (<|channel>, <channel|>, <|tool_call>) in prompt text;
// doing so causes the transformer's attention to autocomplete/repeat the prompt text into thought streams.
const (
	WireGrammarOff = `Operating Guidelines:
- Tool calling: When actions are needed, execute tools directly. Output no internal thought.
- Conversational output: When responding to the user, output pure natural language directly without internal thought.`

	WireGrammarLow = `Operating Guidelines:
- Scratchpad: Use brief thoughts only to verify parameters and paths before executing tools. Keep thoughts concise.
- Tool calling: When invoking tools, execute the necessary tools.
- Conversational output: When responding to the user, output pure natural language directly.`

	WireGrammarMedium = `Operating Guidelines:
- Reasoning: Deliberate step by step. Analyze the task step by step, evaluate tool results, and verify invariants before presenting conclusions.
- Tool calling: When invoking tools, execute the necessary tools. Do not output text outside thought when calling tools.
- Conversational output: When responding to the user, output pure natural language directly.`

	WireGrammarHigh = `Operating Guidelines:
- Deep reasoning: Engage in rigorous, multi-perspective deliberation. Deconstruct the problem, explore edge cases and failure modes, test counter-arguments, and systematically verify code against requirements before taking action or concluding.
- Tool calling: When invoking tools, execute the necessary tools. Do not output text outside thought when calling tools.
- Conversational output: When responding to the user, output pure natural language directly.`
)

// WireGrammarContract is the default medium-level wire contract.
const WireGrammarContract = WireGrammarMedium

var recognizedControlTags = []string{
	"<|channel>thought\n",
	"<|channel>thought\r\n",
	"<|channel>thought<|channel>",
	"<|channel>thought<channel|>",
	"<|channel>thought",
	"<|channel>",
	"<channel|>",
	"</channel>",
	"<|think|>",
	"<think|>",
	"<think>",
	"</think>",
	"<|thought>",
	"<thought|>",
	"<thought>",
	"</thought>",
	"</thought|>",
	"<|tool_call>",
	"<tool_call|>",
	"</tool_call>",
	"<|turn>",
	"<turn|>",
	"</turn>",
	"<|tool>",
	"<tool|>",
	"</tool>",
	"<|tool_response>",
	"<tool_response|>",
	"</tool_response>",
	"<|\"|>",
}

// Gemma4Option allows configuring a Gemma4Codec instance.
type Gemma4Option func(*Gemma4Codec)

// WithReasoningEffort sets the reasoning effort level ("off", "low", "medium", "high").
func WithReasoningEffort(effort string) Gemma4Option {
	return func(c *Gemma4Codec) {
		c.ReasoningEffort = effort
	}
}

// Gemma4Codec implements WireCodec for Gemma 4 models using directional pipe symmetry.
type Gemma4Codec struct {
	ReasoningEffort string
}

// NewGemma4Codec creates an initialized Gemma4Codec with optional configuration.
func NewGemma4Codec(opts ...Gemma4Option) *Gemma4Codec {
	c := &Gemma4Codec{
		ReasoningEffort: "medium",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// isThinkingEnabled reports whether the configured ReasoningEffort enables model thinking.
func (c *Gemma4Codec) isThinkingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(c.ReasoningEffort)) {
	case "off", "none", "disabled":
		return false
	default:
		return true
	}
}

// WireContract returns the appropriate wire contract based on the configured reasoning effort.
func (c *Gemma4Codec) WireContract() string {
	switch strings.ToLower(strings.TrimSpace(c.ReasoningEffort)) {
	case "off", "none", "disabled":
		return WireGrammarOff
	case "low":
		return WireGrammarLow
	case "high":
		return WireGrammarHigh
	default:
		return WireGrammarMedium
	}
}

// StopSequences returns the Ollama options.stop sequences for Gemma 4.
func (c *Gemma4Codec) StopSequences() []string {
	return []string{TagTurnClose, TagToolCallClose}
}

// NewStreamLexer creates a stateful Gemma4StreamLexer instance.
// If ReasoningEffort is "high", the stream lexer initializes in stateInsideThought
// to match the channel-forcing prompt trigger prefix (<|channel>thought\n).
func (c *Gemma4Codec) NewStreamLexer() StreamLexer {
	if strings.ToLower(strings.TrimSpace(c.ReasoningEffort)) == "high" {
		return NewGemma4StreamLexerWithState(stateInsideThought)
	}
	return NewGemma4StreamLexer()
}

// EncodePrompt renders system prompt, tool declarations, and turn history into a deterministic raw prompt string.
// CRITICAL: Sorts all map keys alphabetically so prefix bytes remain 100% identical across turns for KV caching.
func (c *Gemma4Codec) EncodePrompt(sys string, tools []ToolDefinition, history []TurnNode) (string, error) {
	var b strings.Builder

	// Track tool call ID to name mappings across history turns
	toolCallNames := make(map[string]string)
	for _, m := range history {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && tc.Function.Name != "" {
				toolCallNames[tc.ID] = tc.Function.Name
			}
		}
	}

	// Determine active user request boundary
	// Any assistant turn prior to the active user request has its thought block omitted for context hygiene.
	// Any intra-turn hops within the active tool loop preserve their interleaved thoughts.
	activeUserIdx := -1
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role == domain.RoleUser && len(m.Observations) == 0 && !strings.Contains(m.Content, TagToolResponseOpen) {
			activeUserIdx = i
			break
		}
	}

	// Check if history starts with a system message
	histStart := 0
	if sys == "" && len(history) > 0 && history[0].Role == domain.RoleSystem {
		sys = history[0].Content
		histStart = 1
		if activeUserIdx == 0 {
			activeUserIdx = -1
		}
	} else if sys != "" && len(history) > 0 && history[0].Role == domain.RoleSystem {
		histStart = 1
	}

	// 1. System & Tools Block
	// Assemble invariant wire contract and sorted tool declarations FIRST so all /persona
	// roots share a common KV-cache prefix, and place the system/persona block LAST inside
	// <|turn>system immediately before <turn|> for maximum recency salience.
	if sys != "" || len(tools) > 0 {
		b.WriteString(TagTurnOpen + "system\n")
		if c.isThinkingEnabled() && !strings.HasPrefix(strings.TrimSpace(sys), TagThink) {
			b.WriteString(TagThink + "\n")
		}

		if !strings.Contains(sys, "Operating Guidelines:") && !strings.Contains(sys, "directional control tokens") {
			b.WriteString(c.WireContract() + "\n")
		}

		if len(tools) > 0 {
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			// Sort tools alphabetically by name for deterministic prefix bytes
			sortedTools := make([]ToolDefinition, len(tools))
			copy(sortedTools, tools)
			sort.Slice(sortedTools, func(i, j int) bool {
				return sortedTools[i].Name < sortedTools[j].Name
			})

			for _, tool := range sortedTools {
				b.WriteString(FormatToolDeclaration(tool) + "\n")
			}
		}

		if sys != "" {
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			b.WriteString(sys)
		}

		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(TagTurnClose + "\n")
	}

	// 2. Conversation Turn History
	// In Gemma 4's canonical chat template, multi-step tool calls and tool responses execute
	// within a single continuous <|turn>model turn (<|tool_call>...<tool_call|><|tool_response>...<tool_response|>)
	// without closing <turn|> or wrapping <tool_response> in <|turn>user.
	inModelTurn := false
	lastMsgType := ""

	for i := histStart; i < len(history); i++ {
		m := history[i]

		switch m.Role {
		case domain.RoleUser:
			if len(m.Observations) > 0 {
				if !inModelTurn {
					b.WriteString(TagTurnOpen + "model\n")
					inModelTurn = true
				}
				for _, obs := range m.Observations {
					funcName := toolCallNames[obs.ToolCallID]
					if funcName == "" {
						funcName = "tool"
					}
					b.WriteString(FormatToolResponse(funcName, obs.Result))
				}
				lastMsgType = "tool_response"
			} else if strings.Contains(m.Content, TagToolResponseOpen) {
				if !inModelTurn {
					b.WriteString(TagTurnOpen + "model\n")
					inModelTurn = true
				}
				b.WriteString(strings.TrimSpace(m.Content))
				lastMsgType = "tool_response"
			} else {
				if inModelTurn {
					b.WriteString(TagTurnClose + "\n")
					inModelTurn = false
				}
				b.WriteString(TagTurnOpen + "user\n")
				b.WriteString(m.Content)
				b.WriteString(TagTurnClose + "\n")
				lastMsgType = "user"
			}

		case domain.RoleAssistant:
			if !inModelTurn {
				b.WriteString(TagTurnOpen + "model\n")
				inModelTurn = true
			}

			// Check if this assistant turn is an intra-turn hop within the active tool loop
			isIntraTurnHop := activeUserIdx != -1 && i > activeUserIdx
			if isIntraTurnHop && m.Thought != "" && c.isThinkingEnabled() {
				cleanThought := sanitizeThought(m.Thought)
				if cleanThought != "" {
					b.WriteString(TagChannelOpen + "thought\n" + cleanThought + "\n" + TagChannelClose)
				}
			}

			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					b.WriteString(FormatToolCall(tc))
				}
				lastMsgType = "tool_call"
			} else if m.Content != "" {
				cleanContent := sanitizeHistoryContent(m.Content)
				if cleanContent != "" {
					b.WriteString(cleanContent)
					lastMsgType = "assistant_text"
				}
			}

		case domain.RoleTool:
			if !inModelTurn {
				b.WriteString(TagTurnOpen + "model\n")
				inModelTurn = true
			}
			funcName := toolCallNames[m.ToolCallID]
			if funcName == "" {
				funcName = "tool"
			}
			b.WriteString(FormatToolResponse(funcName, m.Content))
			lastMsgType = "tool_response"

		case domain.RoleSummary:
			if inModelTurn {
				b.WriteString(TagTurnClose + "\n")
				inModelTurn = false
			}
			b.WriteString(TagTurnOpen + "user\n")
			b.WriteString("[Conversation Milestone & Summary Context]:\n" + m.Content)
			b.WriteString(TagTurnClose + "\n")
			lastMsgType = "summary"
		}
	}

	// Close completed assistant text turn if it is at the end of history
	if inModelTurn && lastMsgType == "assistant_text" {
		b.WriteString(TagTurnClose + "\n")
		inModelTurn = false
	}

	// 3. Generation Trigger Suffix
	if inModelTurn && lastMsgType == "tool_call" {
		b.WriteString(TagToolResponseOpen)
		return b.String(), nil
	}

	if inModelTurn && lastMsgType == "tool_response" {
		// Continuing an open <|turn>model turn immediately after <tool_response|>
		switch strings.ToLower(strings.TrimSpace(c.ReasoningEffort)) {
		case "off", "none", "disabled":
			b.WriteString(TagChannelOpen + "thought\n" + TagChannelClose)
		case "high":
			b.WriteString(TagChannelOpen + "thought\n")
		}
		return b.String(), nil
	}

	switch strings.ToLower(strings.TrimSpace(c.ReasoningEffort)) {
	case "off", "none", "disabled":
		b.WriteString(TagTurnOpen + "model\n" + TagChannelOpen + "thought\n" + TagChannelClose)
	case "high":
		b.WriteString(TagTurnOpen + "model\n" + TagChannelOpen + "thought\n")
	default:
		b.WriteString(TagTurnOpen + "model\n")
	}

	return b.String(), nil
}

// FormatToolDeclaration formats a tool definition according to Gemma 4 wire grammar:
// <|tool>declaration:{tool_name}{description:<|"|>{desc}<|"|>,parameters:{...}}<tool|>
func FormatToolDeclaration(tool ToolDefinition) string {
	var b strings.Builder
	b.WriteString(TagToolOpen + "declaration:" + tool.Name + "{")
	b.WriteString("description:" + TagStringDelimiter + tool.Description + TagStringDelimiter)

	paramsStr := "{}"
	if tool.Parameters != nil {
		paramsStr = FormatGemmaValue(tool.Parameters)
	}
	b.WriteString(",parameters:" + paramsStr)
	b.WriteString("}" + TagToolClose)
	return b.String()
}

// FormatToolCall formats a domain.ToolCall according to Gemma 4 wire grammar:
// <|tool_call>call:{func_name}{arg1:<|"|>{str_val}<|"|>,arg2:{num_or_bool}}<tool_call|>
func FormatToolCall(tc domain.ToolCall) string {
	argsStr := "{}"
	if len(tc.Function.Arguments) > 0 {
		var parsed interface{}
		if err := json.Unmarshal(tc.Function.Arguments, &parsed); err == nil {
			argsStr = FormatGemmaValue(parsed)
		} else {
			argsStr = string(tc.Function.Arguments)
		}
	}
	return TagToolCallOpen + "call:" + tc.Function.Name + argsStr + TagToolCallClose
}

// FormatToolResponse formats a tool observation turn according to Gemma 4 wire grammar:
// <|tool_response>response:{func_name}{result:<|"|>{observation_payload}<|"|>}<tool_response|>
func FormatToolResponse(funcName, result string) string {
	return TagToolResponseOpen + "response:" + funcName + "{result:" + TagStringDelimiter + result + TagStringDelimiter + "}" + TagToolResponseClose
}

// StripControlTags removes all known control tokens, pseudo-tags, and stray delimiters from text.
func StripControlTags(s string) string {
	for _, tag := range recognizedControlTags {
		s = strings.ReplaceAll(s, tag, "")
	}
	for _, extra := range []string{
		"<|think|>", "<think|>", "<think>", "</think>",
		"<thought>", "</thought>", "<|thought>", "<thought|>", "</thought|>",
		"<channel>", "</channel>", "<|channel>", "<channel|>", "</channel|>",
		"<tool_call>", "</tool_call>", "<|tool_call>", "<tool_call|>",
		"<tool_response>", "</tool_response>", "<|tool_response>", "<tool_response|>",
		"<turn>", "</turn>", "<|turn>", "<turn|>",
		"<tool>", "</tool>", "<|tool>", "<tool|>",
	} {
		s = strings.ReplaceAll(s, extra, "")
	}
	return s
}

// sanitizeHistoryContent cleans prior assistant turn content before encoding into prompt history.
func sanitizeHistoryContent(s string) string {
	cleaned := StripControlTags(s)
	if strings.Trim(cleaned, " \t\r\n>|<") == "" {
		return ""
	}
	return strings.TrimSpace(cleaned)
}

// sanitizeThought cleans text inside a thought block, stripping bare control tags.
func sanitizeThought(s string) string {
	cleaned := StripControlTags(s)
	if strings.Trim(cleaned, " \t\r\n>|<") == "" {
		return ""
	}
	return strings.TrimSpace(cleaned)
}


// FormatGemmaValue recursively formats arbitrary Go structures, maps, and primitives
// into Gemma 4 grammar, sorting all map keys alphabetically for KV cache stability.
func FormatGemmaValue(val interface{}) string {
	if val == nil {
		return "null"
	}

	switch v := val.(type) {
	case string:
		return TagStringDelimiter + v + TagStringDelimiter

	case bool:
		if v {
			return "true"
		}
		return "false"

	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)

	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)

	case float32:
		f := float64(v)
		if math.Floor(f) == f && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'g', -1, 32)

	case float64:
		if math.Floor(v) == v && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)

	case json.Number:
		return v.String()

	case []interface{}:
		var items []string
		for _, elem := range v {
			items = append(items, FormatGemmaValue(elem))
		}
		return "[" + strings.Join(items, ",") + "]"

	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var entries []string
		for _, k := range keys {
			entries = append(entries, k+":"+FormatGemmaValue(v[k]))
		}
		return "{" + strings.Join(entries, ",") + "}"

	case json.RawMessage:
		var parsed interface{}
		if err := json.Unmarshal(v, &parsed); err == nil {
			return FormatGemmaValue(parsed)
		}
		return string(v)

	default:
		// Attempt JSON round-trip for structs or custom types
		data, err := json.Marshal(v)
		if err == nil {
			var parsed interface{}
			if err := json.Unmarshal(data, &parsed); err == nil {
				return FormatGemmaValue(parsed)
			}
		}
		return fmt.Sprintf("%v", v)
	}
}

// State enum for Gemma4StreamLexer
type lexerState int

const (
	stateOutside lexerState = iota
	stateInsideThought
	stateInsideToolCall
)

// Gemma4StreamLexer implements StreamLexer with a sliding lookahead buffer,
// explicit state stack, and thought-bleed protection.
type Gemma4StreamLexer struct {
	state          lexerState
	buf            string
	rehearsalBuf   string
	toolCallBuf    string
	thoughtEmitted bool
	justClosedTag  bool
	channelForced  bool
}

// NewGemma4StreamLexer creates an initialized Gemma4StreamLexer starting in stateOutside.
func NewGemma4StreamLexer() *Gemma4StreamLexer {
	return NewGemma4StreamLexerWithState(stateOutside)
}

// NewGemma4StreamLexerWithState creates an initialized Gemma4StreamLexer starting in the specified state.
func NewGemma4StreamLexerWithState(initialState lexerState) *Gemma4StreamLexer {
	return &Gemma4StreamLexer{
		state:         initialState,
		channelForced: initialState == stateInsideThought,
	}
}

func isPrefixOfRecognizedTag(s string) bool {
	if s == "" {
		return false
	}
	for _, tag := range recognizedControlTags {
		if strings.HasPrefix(tag, s) {
			return true
		}
	}
	return false
}

func isPrefixOfTagFromLastAngle(buf string) (bool, int) {
	angleIdx := strings.LastIndex(buf, "<")
	if angleIdx != -1 && len(buf)-angleIdx <= 24 {
		if isPrefixOfRecognizedTag(buf[angleIdx:]) {
			return true, angleIdx
		}
	}
	return false, -1
}

// Feed receives a chunk from Ollama's response stream and emits parsed StreamEvents.
func (lex *Gemma4StreamLexer) Feed(chunk string) []StreamEvent {
	lex.buf += chunk
	var events []StreamEvent

	for len(lex.buf) > 0 {
		switch lex.state {
		case stateOutside:
			// Strip any stray '>' or '|' immediately following a tag closure across chunk boundaries
			if lex.justClosedTag {
				for len(lex.buf) > 0 && (lex.buf[0] == '>' || lex.buf[0] == '|') {
					lex.buf = lex.buf[1:]
				}
				if len(lex.buf) == 0 {
					return events
				}
				lex.justClosedTag = false
			}

			// 1. Strip leading stray closers or control tokens in stateOutside
			for {
				trimmed := strings.TrimLeft(lex.buf, " \t\r\n")
				strippedAny := false
				for _, closer := range []string{
					TagChannelClose, "</channel>", "</thought>", "<thought|>", "</thought|>",
					TagToolResponseClose, "</tool_response>", TagToolCallClose, "</tool_call>",
					TagToolClose, "</tool>",
				} {
					if strings.HasPrefix(trimmed, closer) {
						trimmed = trimmed[len(closer):]
						for len(trimmed) > 0 && (trimmed[0] == '>' || trimmed[0] == '|' || trimmed[0] == ' ') {
							trimmed = trimmed[1:]
						}
						lex.buf = trimmed
						lex.justClosedTag = true
						strippedAny = true
						break
					}
				}
				if !strippedAny {
					break
				}
			}

			// Also handle pseudo-thought channel opener missing '<|channel>' but ending with '<channel|>'
			// E.g. "thought <channel|>" or "thought\n<channel|>"
			if cIdx := strings.Index(lex.buf, TagChannelClose); cIdx != -1 {
				prefix := strings.TrimSpace(lex.buf[:cIdx])
				if prefix == "" || prefix == "thought" {
					rest := lex.buf[cIdx+len(TagChannelClose):]
					for len(rest) > 0 && (rest[0] == '>' || rest[0] == '|' || rest[0] == ' ') {
						rest = rest[1:]
					}
					lex.buf = strings.TrimLeft(rest, " \t\r\n")
					lex.justClosedTag = true
					continue
				}
			}

			if len(lex.buf) == 0 {
				return events
			}

			// 2. Look for triggers: thought channel opener, <|tool_call>, <turn|>
			thoughtIdx := -1
			thoughtTagLen := 0

			tIdx := strings.Index(lex.buf, TagChannelOpen)
			if tIdx != -1 {
				thoughtIdx = tIdx
				rest := lex.buf[tIdx+len(TagChannelOpen):]
				thoughtTagLen = len(TagChannelOpen)
				if strings.HasPrefix(rest, "thought") {
					thoughtTagLen += len("thought")
					rest = rest[len("thought"):]
				}
				for len(rest) > 0 && (rest[0] == '\r' || rest[0] == '\n' || rest[0] == ' ' || rest[0] == '>' || rest[0] == '|') {
					thoughtTagLen++
					rest = rest[1:]
				}
				if strings.HasPrefix(rest, "<|channel>") {
					thoughtTagLen += len("<|channel>")
				} else if strings.HasPrefix(rest, "<channel|>") {
					thoughtTagLen += len("<channel|>")
				}
			}

			// Also check for alternative thought openers: <thought>, <|thought>, <thought|>
			for _, opener := range []string{"<thought>", "<|thought>", "<thought|>"} {
				idx := strings.Index(lex.buf, opener)
				if idx != -1 && (thoughtIdx == -1 || idx < thoughtIdx) {
					thoughtIdx = idx
					thoughtTagLen = len(opener)
					rest := lex.buf[idx+len(opener):]
					if strings.HasPrefix(rest, "\r\n") {
						thoughtTagLen += 2
					} else if strings.HasPrefix(rest, "\n") {
						thoughtTagLen += 1
					}
				}
			}

			toolCallIdx := strings.Index(lex.buf, TagToolCallOpen)
			if toolCallIdx == -1 {
				toolCallIdx = strings.Index(lex.buf, "<tool_call>")
			}
			turnCloseIdx := strings.Index(lex.buf, TagTurnClose)
			if turnCloseIdx == -1 {
				turnCloseIdx = strings.Index(lex.buf, "</turn>")
			}

			// Find earliest trigger
			earliest := -1
			triggerType := ""
			tagLen := 0

			if thoughtIdx != -1 && (earliest == -1 || thoughtIdx < earliest) {
				earliest = thoughtIdx
				triggerType = "thought"
				tagLen = thoughtTagLen
			}
			if toolCallIdx != -1 && (earliest == -1 || toolCallIdx < earliest) {
				earliest = toolCallIdx
				triggerType = "tool_call"
				tagLen = len(TagToolCallOpen)
				if strings.HasPrefix(lex.buf[toolCallIdx:], "<tool_call>") {
					tagLen = len("<tool_call>")
				}
			}
			if turnCloseIdx != -1 && (earliest == -1 || turnCloseIdx < earliest) {
				earliest = turnCloseIdx
				triggerType = "turn_close"
				tagLen = len(TagTurnClose)
				if strings.HasPrefix(lex.buf[turnCloseIdx:], "</turn>") {
					tagLen = len("</turn>")
				}
			}

			if earliest != -1 {
				if earliest > 0 {
					textChunk := StripControlTags(lex.buf[:earliest])
					if strings.Trim(textChunk, " \t\r\n>|<") != "" {
						events = append(events, StreamEvent{
							Kind: EventTextDelta,
							Text: textChunk,
						})
					}
				}
				lex.buf = lex.buf[earliest+tagLen:]

				switch triggerType {
				case "thought":
					lex.state = stateInsideThought
					lex.thoughtEmitted = false
				case "tool_call":
					lex.state = stateInsideToolCall
					lex.toolCallBuf = ""
				case "turn_close":
					// Model finished turn
				}
				continue
			}

			// No tag found in buffer: check sliding lookahead buffer for unclosed '<' within 24 bytes
			hasPrefix, angleIdx := isPrefixOfTagFromLastAngle(lex.buf)
			if hasPrefix {
				if angleIdx > 0 {
					textChunk := StripControlTags(lex.buf[:angleIdx])
					if strings.Trim(textChunk, " \t\r\n>|<") != "" {
						events = append(events, StreamEvent{
							Kind: EventTextDelta,
							Text: textChunk,
						})
					}
				}
				lex.buf = lex.buf[angleIdx:]
				return events
			}

			// If buffer contains ONLY whitespace, hold it; the next chunk may contain an opener (e.g. <|tool_call> or <|channel>)
			if strings.Trim(lex.buf, " \t\r\n") == "" {
				return events
			}

			// Emit full buffer as text
			textChunk := StripControlTags(lex.buf)
			if textChunk != "" {
				events = append(events, StreamEvent{
					Kind: EventTextDelta,
					Text: textChunk,
				})
			}
			lex.buf = ""
			return events

		case stateInsideThought:
			// If no thought has been emitted yet (e.g. channel-forced stateInsideThought under high effort),
			// strip any redundant "<|channel>thought" or bare "thought\n" opener echoed by the model.
			if !lex.thoughtEmitted {
				trimmed := strings.TrimLeft(lex.buf, " \t\r\n")
				if strings.HasPrefix(trimmed, TagChannelOpen) {
					after := trimmed[len(TagChannelOpen):]
					if len(after) < len("thought") && strings.HasPrefix("thought", after) {
						return events
					}
					if strings.HasPrefix(after, "thought") {
						rest := after[len("thought"):]
						if rest == "" {
							return events
						}
						for len(rest) > 0 && (rest[0] == '\r' || rest[0] == '\n' || rest[0] == ' ') {
							rest = rest[1:]
						}
						if rest != "" && strings.HasPrefix(TagChannelOpen, rest) && rest != TagChannelOpen {
							return events
						}
						if strings.HasPrefix(rest, TagChannelOpen) {
							rest = rest[len(TagChannelOpen):]
						}
						lex.buf = rest
						lex.channelForced = false
						continue
					}
				} else if lex.channelForced {
					if strings.HasPrefix(trimmed, "thought\n") {
						lex.buf = trimmed[len("thought\n"):]
						lex.channelForced = false
						continue
					}
					if strings.HasPrefix(trimmed, "thought\r\n") {
						lex.buf = trimmed[len("thought\r\n"):]
						lex.channelForced = false
						continue
					}
					if trimmed != "" && len(trimmed) <= len("thought\r") && (strings.HasPrefix("thought\n", trimmed) || strings.HasPrefix("thought\r\n", trimmed)) {
						return events
					}
					lex.channelForced = false
				}
			}

			// Look for thought channel closers: <channel|>, <|channel>, </channel>, </thought>, <thought|>, </thought|>
			channelCloseIdx := -1
			closeTagLen := 0
			incompleteOpenerLookahead := false
			for _, closer := range []string{TagChannelClose, TagChannelOpen, "</channel>", "</thought>", "<thought|>", "</thought|>"} {
				idx := strings.Index(lex.buf, closer)
				if idx != -1 {
					if closer == TagChannelOpen {
						after := lex.buf[idx+len(TagChannelOpen):]
						if len(after) < len("thought") && strings.HasPrefix("thought", after) {
							incompleteOpenerLookahead = true
							continue
						}
					}
					if channelCloseIdx == -1 || idx < channelCloseIdx {
						channelCloseIdx = idx
						closeTagLen = len(closer)
					}
				}
			}
			if channelCloseIdx == -1 && incompleteOpenerLookahead {
				return events
			}

			toolCallIdx := strings.Index(lex.buf, TagToolCallOpen)

			if channelCloseIdx != -1 && (toolCallIdx == -1 || channelCloseIdx < toolCallIdx) {
				// Thought is closing
				if lex.rehearsalBuf != "" {
					events = append(events, StreamEvent{
						Kind: EventThoughtDelta,
						Text: lex.rehearsalBuf,
					})
					lex.rehearsalBuf = ""
					lex.thoughtEmitted = true
				}
				if channelCloseIdx > 0 {
					thoughtSegment := lex.buf[:channelCloseIdx]
					// Strip any trailing "thought\n" or "thought" channel echo right before <channel|>
					for _, suffix := range []string{"thought\r\n", "thought\n", "thought"} {
						if strings.HasSuffix(thoughtSegment, suffix) {
							thoughtSegment = thoughtSegment[:len(thoughtSegment)-len(suffix)]
							break
						}
					}
					// Suppress bare "thought" channel echo when model immediately closes an empty thought channel
					if thoughtSegment != "" && (lex.thoughtEmitted || strings.TrimSpace(thoughtSegment) != "thought") {
						events = append(events, StreamEvent{
							Kind: EventThoughtDelta,
							Text: thoughtSegment,
						})
						lex.thoughtEmitted = true
					}
				}
				rest := lex.buf[channelCloseIdx+closeTagLen:]
				for len(rest) > 0 && (rest[0] == '>' || rest[0] == '|') {
					rest = rest[1:]
				}
				lex.buf = rest
				lex.state = stateOutside
				lex.justClosedTag = true
				continue
			}

			if toolCallIdx != -1 {
				// Tool call syntax inside thought: could be model rehearsal or unclosed-thought tool call
				if toolCallIdx > 0 {
					if lex.rehearsalBuf != "" {
						events = append(events, StreamEvent{
							Kind: EventThoughtDelta,
							Text: lex.rehearsalBuf,
						})
						lex.rehearsalBuf = ""
						lex.thoughtEmitted = true
					}
					events = append(events, StreamEvent{
						Kind: EventThoughtDelta,
						Text: lex.buf[:toolCallIdx],
					})
					lex.thoughtEmitted = true
					lex.buf = lex.buf[toolCallIdx:]
				}

				// Check if <tool_call|> or </tool_call> is also in buffer
				toolCallCloseIdx := strings.Index(lex.buf, TagToolCallClose)
				toolCallCloseLen := len(TagToolCallClose)
				if toolCallCloseIdx == -1 {
					toolCallCloseIdx = strings.Index(lex.buf, "</tool_call>")
					if toolCallCloseIdx != -1 {
						toolCallCloseLen = len("</tool_call>")
					}
				}

				if toolCallCloseIdx != -1 {
					callEnd := toolCallCloseIdx + toolCallCloseLen
					if lex.rehearsalBuf != "" {
						events = append(events, StreamEvent{
							Kind: EventThoughtDelta,
							Text: lex.rehearsalBuf,
						})
						lex.thoughtEmitted = true
					}
					// Hold tool call in rehearsal buffer
					lex.rehearsalBuf = lex.buf[:callEnd]
					lex.buf = lex.buf[callEnd:]
					continue
				}

				// Waiting for tool call closure
				return events
			}

			// Neither closer nor <|tool_call> found
			hasPrefix, angleIdx := isPrefixOfTagFromLastAngle(lex.buf)
			if hasPrefix {
				if angleIdx > 0 {
					if lex.rehearsalBuf != "" {
						events = append(events, StreamEvent{
							Kind: EventThoughtDelta,
							Text: lex.rehearsalBuf,
						})
						lex.rehearsalBuf = ""
						lex.thoughtEmitted = true
					}
					events = append(events, StreamEvent{
						Kind: EventThoughtDelta,
						Text: lex.buf[:angleIdx],
					})
					lex.thoughtEmitted = true
				}
				lex.buf = lex.buf[angleIdx:]
				return events
			}

			// Emit thought buffer
			if lex.rehearsalBuf != "" && len(lex.buf) > 0 {
				events = append(events, StreamEvent{
					Kind: EventThoughtDelta,
					Text: lex.rehearsalBuf,
				})
				lex.rehearsalBuf = ""
				lex.thoughtEmitted = true
			}
			events = append(events, StreamEvent{
				Kind: EventThoughtDelta,
				Text: lex.buf,
			})
			lex.thoughtEmitted = true
			lex.buf = ""
			return events

		case stateInsideToolCall:
			toolCallCloseIdx := strings.Index(lex.buf, TagToolCallClose)
			toolCallCloseLen := len(TagToolCallClose)
			if toolCallCloseIdx == -1 {
				toolCallCloseIdx = strings.Index(lex.buf, "</tool_call>")
				if toolCallCloseIdx != -1 {
					toolCallCloseLen = len("</tool_call>")
				}
			}

			if toolCallCloseIdx != -1 {
				lex.toolCallBuf += lex.buf[:toolCallCloseIdx]
				rest := lex.buf[toolCallCloseIdx+toolCallCloseLen:]
				for len(rest) > 0 && (rest[0] == '>' || rest[0] == '|') {
					rest = rest[1:]
				}
				lex.buf = rest

				tc, err := parseGemmaToolCall(lex.toolCallBuf)
				if err == nil && tc != nil {
					events = append(events, StreamEvent{
						Kind:     EventToolCall,
						ToolCall: tc,
					})
				}
				lex.toolCallBuf = ""
				lex.state = stateOutside
				lex.justClosedTag = true
				continue
			}

			hasPrefix, angleIdx := isPrefixOfTagFromLastAngle(lex.buf)
			if hasPrefix {
				lex.toolCallBuf += lex.buf[:angleIdx]
				lex.buf = lex.buf[angleIdx:]
				return events
			}

			lex.toolCallBuf += lex.buf
			lex.buf = ""
			return events
		}
	}

	return events
}

// Close flushes buffered state at stream completion and applies the unclosed-thought fallback.
func (lex *Gemma4StreamLexer) Close() ([]StreamEvent, error) {
	var events []StreamEvent

	switch lex.state {
	case stateInsideThought:
		// Rule 3: Only promote an unclosed-thought tool call as a fallback if and only if
		// the stream hits EOS (done: true) immediately after <tool_call|> without closing <channel|>.
		if lex.rehearsalBuf != "" {
			tc, err := parseGemmaToolCall(lex.rehearsalBuf)
			if err == nil && tc != nil {
				events = append(events, StreamEvent{
					Kind:     EventToolCall,
					ToolCall: tc,
				})
				lex.rehearsalBuf = ""
				lex.state = stateOutside
			} else {
				events = append(events, StreamEvent{
					Kind: EventThoughtDelta,
					Text: lex.rehearsalBuf,
				})
				lex.rehearsalBuf = ""
			}
		}

		if len(lex.buf) > 0 {
			if strings.Contains(lex.buf, TagToolCallOpen) {
				idx := strings.Index(lex.buf, TagToolCallOpen)
				if idx > 0 {
					events = append(events, StreamEvent{
						Kind: EventThoughtDelta,
						Text: lex.buf[:idx],
					})
				}
				tc, err := parseGemmaToolCall(lex.buf[idx:])
				if err == nil && tc != nil {
					events = append(events, StreamEvent{
						Kind:     EventToolCall,
						ToolCall: tc,
					})
					lex.state = stateOutside
					lex.buf = ""
				}
			}

			if len(lex.buf) > 0 {
				cleaned := StripControlTags(lex.buf)
				if strings.Trim(cleaned, " \t\r\n>|<") != "" {
					events = append(events, StreamEvent{
						Kind: EventThoughtDelta,
						Text: strings.TrimSpace(cleaned),
					})
				}
				lex.buf = ""
			}
		}

	case stateInsideToolCall:
		lex.toolCallBuf += lex.buf
		lex.buf = ""
		tc, err := parseGemmaToolCall(lex.toolCallBuf)
		if err == nil && tc != nil {
			events = append(events, StreamEvent{
				Kind:     EventToolCall,
				ToolCall: tc,
			})
		}
		lex.toolCallBuf = ""
		lex.state = stateOutside

	case stateOutside:
		if len(lex.buf) > 0 {
			cleaned := StripControlTags(lex.buf)
			if strings.Trim(cleaned, " \t\r\n>|<") != "" {
				events = append(events, StreamEvent{
					Kind: EventTextDelta,
					Text: cleaned,
				})
			}
			lex.buf = ""
		}
	}

	return events, nil
}

var unquotedKeyRegex = regexp.MustCompile(`([{,]\s*)([a-zA-Z0-9_]+)\s*:`)

// parseGemmaToolCall parses call:func_name{...} or func_name{...} syntax into a domain.ToolCall.
func parseGemmaToolCall(raw string) (*domain.ToolCall, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, TagToolCallOpen)
	s = strings.TrimSuffix(s, TagToolCallClose)
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "call:") {
		s = strings.TrimPrefix(s, "call:")
		s = strings.TrimSpace(s)
	}

	braceIdx := strings.Index(s, "{")
	if braceIdx == -1 {
		return nil, fmt.Errorf("no argument block found in tool call: %q", raw)
	}

	funcName := strings.TrimSpace(s[:braceIdx])
	rawArgs := strings.TrimSpace(s[braceIdx:])

	// Try custom Gemma 4 parser handling <|"|> string delimiters
	parsedMap, err := parseGemmaObject(rawArgs)
	if err != nil {
		// Defensive fallback: pseudo-JSON normalization
		normJSON := normalizePseudoJSON(rawArgs)
		if err2 := json.Unmarshal([]byte(normJSON), &parsedMap); err2 != nil {
			return nil, fmt.Errorf("failed to parse tool arguments: %w (fallback err: %v)", err, err2)
		}
	}

	jsonBytes, err := json.Marshal(parsedMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parsed arguments: %w", err)
	}

	callID := fmt.Sprintf("call_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
	return &domain.ToolCall{
		ID:   callID,
		Type: "function",
		Function: struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}{
			Name:      funcName,
			Arguments: jsonBytes,
		},
	}, nil
}

// normalizePseudoJSON converts model pseudo-JSON into valid JSON as a fallback
func normalizePseudoJSON(rawArgs string) string {
	s := strings.TrimSpace(rawArgs)
	s = strings.ReplaceAll(s, TagStringDelimiter, `"`)
	if !strings.HasPrefix(s, "{") {
		s = "{" + s
	}
	if !strings.HasSuffix(s, "}") {
		s = s + "}"
	}
	s = unquotedKeyRegex.ReplaceAllString(s, `$1"$2":`)
	return s
}

// parseGemmaObject parses a `{key:val,...}` string respecting `<|"|>` string boundaries.
func parseGemmaObject(s string) (map[string]interface{}, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, fmt.Errorf("object must start with '{' and end with '}'")
	}

	inner := strings.TrimSpace(s[1 : len(s)-1])
	res := make(map[string]interface{})
	if inner == "" {
		return res, nil
	}

	p := 0
	for p < len(inner) {
		// Skip whitespace
		for p < len(inner) && (inner[p] == ' ' || inner[p] == '\t' || inner[p] == '\n' || inner[p] == '\r') {
			p++
		}
		if p >= len(inner) {
			break
		}

		// Read key
		keyStart := p
		for p < len(inner) && inner[p] != ':' && inner[p] != ' ' && inner[p] != '\t' && inner[p] != '\n' {
			p++
		}
		key := strings.Trim(strings.TrimSpace(inner[keyStart:p]), `"`)

		// Skip to ':'
		for p < len(inner) && inner[p] != ':' {
			p++
		}
		if p >= len(inner) || inner[p] != ':' {
			return nil, fmt.Errorf("missing ':' after key %q", key)
		}
		p++ // Skip ':'

		// Skip whitespace
		for p < len(inner) && (inner[p] == ' ' || inner[p] == '\t' || inner[p] == '\n' || inner[p] == '\r') {
			p++
		}
		if p >= len(inner) {
			return nil, fmt.Errorf("missing value for key %q", key)
		}

		// Parse value
		val, newP, err := parseGemmaValue(inner, p)
		if err != nil {
			return nil, fmt.Errorf("failed to parse value for key %q: %w", key, err)
		}
		res[key] = val
		p = newP

		// Skip whitespace
		for p < len(inner) && (inner[p] == ' ' || inner[p] == '\t' || inner[p] == '\n' || inner[p] == '\r') {
			p++
		}
		if p < len(inner) && inner[p] == ',' {
			p++ // Skip ','
		}
	}

	return res, nil
}

// parseGemmaValue parses a single value starting at index p in s.
func parseGemmaValue(s string, p int) (interface{}, int, error) {
	if p >= len(s) {
		return nil, p, fmt.Errorf("unexpected end of input")
	}

	// 1. String enclosed in <|"|>...<|"|>
	if strings.HasPrefix(s[p:], TagStringDelimiter) {
		start := p + len(TagStringDelimiter)
		end := strings.Index(s[start:], TagStringDelimiter)
		if end == -1 {
			return nil, p, fmt.Errorf("unclosed string delimiter %s", TagStringDelimiter)
		}
		strVal := s[start : start+end]
		return strVal, start + end + len(TagStringDelimiter), nil
	}

	// 2. Standard quoted string "..."
	if s[p] == '"' {
		start := p + 1
		escaped := false
		end := -1
		for i := start; i < len(s); i++ {
			if escaped {
				escaped = false
				continue
			}
			if s[i] == '\\' {
				escaped = true
				continue
			}
			if s[i] == '"' {
				end = i
				break
			}
		}
		if end == -1 {
			return nil, p, fmt.Errorf("unclosed quote string")
		}
		var strVal string
		_ = json.Unmarshal([]byte(s[p:end+1]), &strVal)
		return strVal, end + 1, nil
	}

	// 3. Nested Object {...}
	if s[p] == '{' {
		depth := 0
		start := p
		inStr := false
		for p < len(s) {
			if strings.HasPrefix(s[p:], TagStringDelimiter) {
				inStr = !inStr
				p += len(TagStringDelimiter)
				continue
			}
			if !inStr {
				if s[p] == '{' {
					depth++
				} else if s[p] == '}' {
					depth--
					if depth == 0 {
						p++
						obj, err := parseGemmaObject(s[start:p])
						return obj, p, err
					}
				}
			}
			p++
		}
		return nil, p, fmt.Errorf("unclosed object '{'")
	}

	// 4. Nested Array [...]
	if s[p] == '[' {
		depth := 0
		start := p
		inStr := false
		for p < len(s) {
			if strings.HasPrefix(s[p:], TagStringDelimiter) {
				inStr = !inStr
				p += len(TagStringDelimiter)
				continue
			}
			if !inStr {
				if s[p] == '[' {
					depth++
				} else if s[p] == ']' {
					depth--
					if depth == 0 {
						p++
						arr, err := parseGemmaArray(s[start:p])
						return arr, p, err
					}
				}
			}
			p++
		}
		return nil, p, fmt.Errorf("unclosed array '['")
	}

	// 5. Primitive: boolean, null, or number
	start := p
	for p < len(s) && s[p] != ',' && s[p] != '}' && s[p] != ']' && s[p] != ' ' && s[p] != '\t' && s[p] != '\n' && s[p] != '\r' {
		p++
	}
	token := s[start:p]

	switch token {
	case "true":
		return true, p, nil
	case "false":
		return false, p, nil
	case "null":
		return nil, p, nil
	}

	if intVal, err := strconv.ParseInt(token, 10, 64); err == nil {
		return intVal, p, nil
	}
	if floatVal, err := strconv.ParseFloat(token, 64); err == nil {
		return floatVal, p, nil
	}

	return token, p, nil
}

// parseGemmaArray parses `[elem1,elem2,...]` respecting `<|"|>` string boundaries.
func parseGemmaArray(s string) ([]interface{}, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("array must start with '[' and end with ']'")
	}

	inner := strings.TrimSpace(s[1 : len(s)-1])
	var res []interface{}
	if inner == "" {
		return res, nil
	}

	p := 0
	for p < len(inner) {
		for p < len(inner) && (inner[p] == ' ' || inner[p] == '\t' || inner[p] == '\n' || inner[p] == '\r') {
			p++
		}
		if p >= len(inner) {
			break
		}

		val, newP, err := parseGemmaValue(inner, p)
		if err != nil {
			return nil, err
		}
		res = append(res, val)
		p = newP

		for p < len(inner) && (inner[p] == ' ' || inner[p] == '\t' || inner[p] == '\n' || inner[p] == '\r') {
			p++
		}
		if p < len(inner) && inner[p] == ',' {
			p++
		}
	}

	return res, nil
}
