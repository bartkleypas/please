package wire_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/providers/wire"
)

func TestGemma4Codec_StopSequences(t *testing.T) {
	codec := wire.NewGemma4Codec()
	stops := codec.StopSequences()
	if len(stops) != 2 || stops[0] != "<turn|>" || stops[1] != "<tool_call|>" {
		t.Fatalf("unexpected stop sequences: %+v", stops)
	}
}

func TestGemma4Codec_EncodePrompt_SystemAndToolsDeterministicSorting(t *testing.T) {
	codec := wire.NewGemma4Codec()

	toolsA := []wire.ToolDefinition{
		{
			Name:        "zebra_tool",
			Description: "Handles zebras",
			Parameters: map[string]interface{}{
				"z": "last",
				"a": "first",
				"m": map[string]interface{}{
					"y": 2,
					"b": 1,
				},
			},
		},
		{
			Name:        "alpha_tool",
			Description: "Handles alpha",
			Parameters: map[string]interface{}{
				"path": "test.txt",
			},
		},
	}

	// Inverse order input
	toolsB := []wire.ToolDefinition{
		toolsA[1],
		toolsA[0],
	}

	promptA, err := codec.EncodePrompt("System instruction.", toolsA, nil)
	if err != nil {
		t.Fatalf("EncodePrompt A failed: %v", err)
	}

	promptB, err := codec.EncodePrompt("System instruction.", toolsB, nil)
	if err != nil {
		t.Fatalf("EncodePrompt B failed: %v", err)
	}

	// Byte-for-byte identity check across turns for KV caching
	if promptA != promptB {
		t.Fatalf("KV-cache violation: promptA != promptB!\nPromptA:\n%s\nPromptB:\n%s", promptA, promptB)
	}

	// Verify alpha_tool comes before zebra_tool
	alphaIdx := strings.Index(promptA, "declaration:alpha_tool")
	zebraIdx := strings.Index(promptA, "declaration:zebra_tool")
	if alphaIdx == -1 || zebraIdx == -1 || alphaIdx >= zebraIdx {
		t.Fatalf("expected alpha_tool before zebra_tool, got alphaIdx=%d, zebraIdx=%d", alphaIdx, zebraIdx)
	}

	// Verify map keys sorted inside parameters
	expectedSub := "parameters:{a:<|\"|>first<|\"|>,m:{b:1,y:2},z:<|\"|>last<|\"|>}"
	if !strings.Contains(promptA, expectedSub) {
		t.Fatalf("expected sorted parameters %q, got prompt:\n%s", expectedSub, promptA)
	}

	// Verify trigger suffix
	if !strings.HasSuffix(promptA, "<|turn>model\n") {
		t.Fatalf("expected prompt to end with <|turn>model\\n, got: %q", promptA)
	}
}

func TestGemma4Codec_EncodePrompt_MultiTurnHistoryAndThoughtHygiene(t *testing.T) {
	codec := wire.NewGemma4Codec()

	// Turn 1 (Historical completed turn):
	// User: "First question"
	// Assistant: Thought: "Secret historical thought", Content: "First answer"
	//
	// Turn 2 (Active turn):
	// User: "Second question"
	// Assistant (Intra-turn hop): Thought: "Active working thought", Content: "Running tool", ToolCall: search
	// User (Tool Observation): Result: "Search result 42"
	history := []wire.TurnNode{
		{
			Role:    domain.RoleUser,
			Content: "First question",
		},
		{
			Role:    domain.RoleAssistant,
			Thought: "Secret historical thought",
			Content: "First answer",
		},
		{
			Role:    domain.RoleUser,
			Content: "Second question",
		},
		{
			Role:    domain.RoleAssistant,
			Thought: "Active working thought",
			Content: "Running tool",
			ToolCalls: []domain.ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "search",
						Arguments: json.RawMessage(`{"query":"test"}`),
					},
				},
			},
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "call_123",
			Content:    "Search result 42",
		},
	}

	prompt, err := codec.EncodePrompt("System prompt.", nil, history)
	if err != nil {
		t.Fatalf("EncodePrompt failed: %v", err)
	}

	// 1. Context Hygiene: Historical thought MUST be omitted
	if strings.Contains(prompt, "Secret historical thought") {
		t.Fatalf("historical thought leaked into prompt! Prompt:\n%s", prompt)
	}

	// 2. Intra-Turn Working Memory Continuity: Active working thought MUST be preserved
	if !strings.Contains(prompt, "<|channel>thought\nActive working thought\n<channel|>") {
		t.Fatalf("active intra-turn thought missing from prompt! Prompt:\n%s", prompt)
	}

	// 3. Tool Call and Tool Observation inside continuous <|turn>model turn (no <|turn>user wrapper)
	expectedToolLoop := `<|channel>thought` + "\n" + `Active working thought` + "\n" + `<channel|><|tool_call>call:search{query:<|"|>test<|"|>}<tool_call|><|tool_response>response:search{result:<|"|>Search result 42<|"|>}<tool_response|>`
	if !strings.Contains(prompt, expectedToolLoop) {
		t.Fatalf("canonical single-model-turn tool loop missing or malformed in prompt! Prompt:\n%s", prompt)
	}

	// 4. Generation trigger suffix: remains open right after <tool_response|> inside <|turn>model
	if !strings.HasSuffix(prompt, "<tool_response|>") {
		t.Fatalf("expected open model turn ending with <tool_response|>, got:\n%s", prompt)
	}
}

func TestGemma4StreamLexer_InterleavedThoughtTextToolCall(t *testing.T) {
	lexer := wire.NewGemma4StreamLexer()

	input := "<|channel>thought\nLet me plan my actions.\n<channel|>" +
		"I will now inspect the repository.\n" +
		`<|tool_call>call:list_files{path:<|"|>src<|"|>}<tool_call|>`

	events := lexer.Feed(input)
	closeEvents, err := lexer.Close()
	if err != nil {
		t.Fatalf("lexer.Close failed: %v", err)
	}
	events = append(events, closeEvents...)

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}

	// 1. ThoughtDelta
	if events[0].Kind != wire.EventThoughtDelta || events[0].Text != "Let me plan my actions.\n" {
		t.Errorf("event 0 unexpected: %+v", events[0])
	}

	// 2. TextDelta
	if events[1].Kind != wire.EventTextDelta || events[1].Text != "I will now inspect the repository.\n" {
		t.Errorf("event 1 unexpected: %+v", events[1])
	}

	// 3. ToolCall
	if events[2].Kind != wire.EventToolCall || events[2].ToolCall == nil {
		t.Fatalf("event 2 unexpected: %+v", events[2])
	}
	if events[2].ToolCall.Function.Name != "list_files" {
		t.Errorf("expected tool name list_files, got %s", events[2].ToolCall.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(events[2].ToolCall.Function.Arguments, &args); err != nil {
		t.Fatalf("failed to unmarshal tool call arguments: %v", err)
	}
	if args["path"] != "src" {
		t.Errorf("expected path 'src', got %v", args["path"])
	}
}

func TestGemma4StreamLexer_SplitChunkTags(t *testing.T) {
	chunks := []string{
		"Hello world. <|chan",
		"nel>thought\nThinking step 1... <chan",
		"nel|>Thinking completed. <|tool_",
		`call>call:exec{cmd:<|"|>ls -l`,
		`<|"|>}<tool_`,
		"call|>",
	}

	lexer := wire.NewGemma4StreamLexer()
	var allEvents []wire.StreamEvent

	for _, chunk := range chunks {
		evs := lexer.Feed(chunk)
		allEvents = append(allEvents, evs...)
	}

	closeEvents, err := lexer.Close()
	if err != nil {
		t.Fatalf("lexer.Close failed: %v", err)
	}
	allEvents = append(allEvents, closeEvents...)

	var textParts []string
	var thoughtParts []string
	var toolCalls []*domain.ToolCall

	for _, ev := range allEvents {
		switch ev.Kind {
		case wire.EventTextDelta:
			textParts = append(textParts, ev.Text)
		case wire.EventThoughtDelta:
			thoughtParts = append(thoughtParts, ev.Text)
		case wire.EventToolCall:
			toolCalls = append(toolCalls, ev.ToolCall)
		}
	}

	fullText := strings.Join(textParts, "")
	fullThought := strings.Join(thoughtParts, "")

	if fullText != "Hello world. Thinking completed. " {
		t.Errorf("unexpected fullText: %q", fullText)
	}
	if fullThought != "Thinking step 1... " {
		t.Errorf("unexpected fullThought: %q", fullThought)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "exec" {
		t.Errorf("expected exec, got %s", toolCalls[0].Function.Name)
	}
}

func TestGemma4StreamLexer_ThoughtBleed_RehearsalDuringThought(t *testing.T) {
	// Case A: Model rehearses a tool call in the middle of thinking, then continues thinking and closes channel.
	input := "<|channel>thought\n" +
		"I could try calling `<|tool_call>call:rehearsal_tool{x:1}<tool_call|>` but that might fail.\n" +
		"Instead, I should read the file.\n" +
		"<channel|>\n" +
		"Okay, executing real tool now.\n" +
		`<|tool_call>call:real_tool{y:2}<tool_call|>`

	lexer := wire.NewGemma4StreamLexer()
	events := lexer.Feed(input)
	closeEvents, err := lexer.Close()
	if err != nil {
		t.Fatalf("lexer.Close failed: %v", err)
	}
	events = append(events, closeEvents...)

	var toolCalls []*domain.ToolCall
	var thoughtBuf strings.Builder

	for _, ev := range events {
		if ev.Kind == wire.EventThoughtDelta {
			thoughtBuf.WriteString(ev.Text)
		} else if ev.Kind == wire.EventToolCall {
			toolCalls = append(toolCalls, ev.ToolCall)
		}
	}

	// Rehearsal should be kept inside thought
	thoughtStr := thoughtBuf.String()
	if !strings.Contains(thoughtStr, "call:rehearsal_tool") {
		t.Errorf("expected rehearsal to be preserved in thought, got: %q", thoughtStr)
	}

	// Only 1 real tool call should be emitted
	if len(toolCalls) != 1 {
		t.Fatalf("expected exactly 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "real_tool" {
		t.Errorf("expected real_tool, got %s", toolCalls[0].Function.Name)
	}
}

func TestGemma4StreamLexer_ThoughtBleed_UnclosedThoughtFallbackAtEOS(t *testing.T) {
	// Case B: Model starts thinking, puts tool call at end of thought, and stream hits EOS without <channel|>.
	input := "<|channel>thought\n" +
		"I have determined the solution.\n" +
		`<|tool_call>call:finish_task{status:<|"|>done<|"|>}<tool_call|>`

	lexer := wire.NewGemma4StreamLexer()
	events := lexer.Feed(input)
	closeEvents, err := lexer.Close()
	if err != nil {
		t.Fatalf("lexer.Close failed: %v", err)
	}
	events = append(events, closeEvents...)

	var toolCalls []*domain.ToolCall
	var thoughtBuf strings.Builder

	for _, ev := range events {
		if ev.Kind == wire.EventThoughtDelta {
			thoughtBuf.WriteString(ev.Text)
		} else if ev.Kind == wire.EventToolCall {
			toolCalls = append(toolCalls, ev.ToolCall)
		}
	}

	// Thought should contain the planning text, but NOT the promoted tool call
	thoughtStr := thoughtBuf.String()
	if strings.Contains(thoughtStr, "finish_task") {
		t.Errorf("promoted tool call leaked into thought block: %q", thoughtStr)
	}
	if !strings.Contains(thoughtStr, "I have determined the solution.") {
		t.Errorf("thought missing expected planning text: %q", thoughtStr)
	}

	// The unclosed thought tool call MUST be promoted
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 promoted tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "finish_task" {
		t.Errorf("expected finish_task, got %s", toolCalls[0].Function.Name)
	}
}

func TestGemma4StreamLexer_NormalProseWithAngleBracket(t *testing.T) {
	lexer := wire.NewGemma4StreamLexer()

	chunks := []string{
		"Condition: if x <",
		" 5 && y > 10 {\n",
		"  return <div>Hello</div>;\n",
		"}\n",
	}

	var textParts []string
	for _, chunk := range chunks {
		evs := lexer.Feed(chunk)
		for _, ev := range evs {
			if ev.Kind == wire.EventTextDelta {
				textParts = append(textParts, ev.Text)
			}
		}
	}
	closeEvs, err := lexer.Close()
	if err != nil {
		t.Fatalf("lexer.Close failed: %v", err)
	}
	for _, ev := range closeEvs {
		if ev.Kind == wire.EventTextDelta {
			textParts = append(textParts, ev.Text)
		}
	}

	fullText := strings.Join(textParts, "")
	expected := "Condition: if x < 5 && y > 10 {\n  return <div>Hello</div>;\n}\n"
	if fullText != expected {
		t.Fatalf("expected %q, got %q", expected, fullText)
	}
}

func TestGemma4Codec_EncodePrompt_SpecialCharactersInStrings(t *testing.T) {
	codec := wire.NewGemma4Codec()

	// String containing quotes, colons, newlines, and braces
	trickyContent := "Line 1: \"quoted text\"\nLine 2: {key: value}\nLine 3: <div>test</div>"

	history := []wire.TurnNode{
		{
			Role:    domain.RoleUser,
			Content: trickyContent,
		},
		{
			Role:    domain.RoleAssistant,
			Content: "Acknowledged",
			ToolCalls: []domain.ToolCall{
				{
					ID:   "call_special",
					Type: "function",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "write_file",
						Arguments: json.RawMessage(`{"content":"hello \"world\"\nnext line","path":"/path/to/{file}.go"}`),
					},
				},
			},
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "call_special",
			Content:    "Success with {braces} and \"quotes\"",
		},
	}

	prompt, err := codec.EncodePrompt("", nil, history)
	if err != nil {
		t.Fatalf("EncodePrompt failed: %v", err)
	}

	// Verify tool call argument formatting with <|"|>
	expectedArg1 := `content:<|"|>hello "world"` + "\n" + `next line<|"|>`
	expectedArg2 := `path:<|"|>/path/to/{file}.go<|"|>`
	if !strings.Contains(prompt, expectedArg1) {
		t.Errorf("missing formatted arg1 %q in prompt:\n%s", expectedArg1, prompt)
	}
	if !strings.Contains(prompt, expectedArg2) {
		t.Errorf("missing formatted arg2 %q in prompt:\n%s", expectedArg2, prompt)
	}

	// Verify observation response
	expectedObs := `response:write_file{result:<|"|>Success with {braces} and "quotes"<|"|>}`
	if !strings.Contains(prompt, expectedObs) {
		t.Errorf("missing observation %q in prompt:\n%s", expectedObs, prompt)
	}
}

func TestGemma4StreamLexer_LexSpecialStringToolCall(t *testing.T) {
	lexer := wire.NewGemma4StreamLexer()

	// Tool call containing quotes and newlines inside <|"|>
	chunk := `<|tool_call>call:write_file{content:<|"|>hello "world"` + "\n" + `next line<|"|>,path:<|"|>foo.go<|"|>}<tool_call|>`

	events := lexer.Feed(chunk)
	closeEvents, err := lexer.Close()
	if err != nil {
		t.Fatalf("lexer.Close failed: %v", err)
	}
	events = append(events, closeEvents...)

	if len(events) != 1 || events[0].Kind != wire.EventToolCall {
		t.Fatalf("expected 1 ToolCall event, got %+v", events)
	}

	tc := events[0].ToolCall
	if tc.Function.Name != "write_file" {
		t.Errorf("expected write_file, got %s", tc.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
		t.Fatalf("failed to unmarshal args: %v", err)
	}
	expectedContent := "hello \"world\"\nnext line"
	if args["content"] != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, args["content"])
	}
	if args["path"] != "foo.go" {
		t.Errorf("expected path 'foo.go', got %v", args["path"])
	}
}

func TestGemma4StreamLexer_ChannelThoughtEnvelopeVariants(t *testing.T) {
	// Observed in gemma4:26b-mlx trace:
	// The model emits "<|channel>thought<|channel>" without a newline,
	// and might close with "<|channel>" or "<channel|>".
	testCases := []struct {
		name            string
		chunks          []string
		expectedThought string
		expectedText    string
		expectedTools   int
	}{
		{
			name: "Envelope without newline",
			chunks: []string{
				"<|channel>thought<|channel>",
				"I need to recall recent memories.\n",
				"<|channel>",
				"I have recalled the context.",
			},
			expectedThought: "I need to recall recent memories.\n",
			expectedText:    "I have recalled the context.",
			expectedTools:   0,
		},
		{
			name: "Envelope with mixed closer and tool call",
			chunks: []string{
				"<|channel>thought<channel|>",
				"Analyzing user query\n",
				"<channel|>",
				"I will run the command.\n",
				`<|tool_call>call:run_cmd{cmd:<|"|>ls<|"|>}<tool_call|>`,
			},
			expectedThought: "Analyzing user query\n",
			expectedText:    "I will run the command.\n",
			expectedTools:   1,
		},
		{
			name: "Standard pipe format with space",
			chunks: []string{
				"<|channel>thought ",
				"Deep reasoning here.",
				"<channel|>",
				"Here is the answer.",
			},
			expectedThought: "Deep reasoning here.",
			expectedText:    "Here is the answer.",
			expectedTools:   0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lexer := wire.NewGemma4StreamLexer()
			var allEvents []wire.StreamEvent
			for _, chunk := range tc.chunks {
				allEvents = append(allEvents, lexer.Feed(chunk)...)
			}
			closeEvents, err := lexer.Close()
			if err != nil {
				t.Fatalf("Close failed: %v", err)
			}
			allEvents = append(allEvents, closeEvents...)

			var thoughtParts, textParts []string
			var toolCalls int
			for _, ev := range allEvents {
				switch ev.Kind {
				case wire.EventThoughtDelta:
					thoughtParts = append(thoughtParts, ev.Text)
				case wire.EventTextDelta:
					textParts = append(textParts, ev.Text)
				case wire.EventToolCall:
					toolCalls++
				}
			}

			fullThought := strings.Join(thoughtParts, "")
			fullText := strings.Join(textParts, "")

			if fullThought != tc.expectedThought {
				t.Errorf("expected thought %q, got %q", tc.expectedThought, fullThought)
			}
			if fullText != tc.expectedText {
				t.Errorf("expected text %q, got %q", tc.expectedText, fullText)
			}
			if toolCalls != tc.expectedTools {
				t.Errorf("expected %d tool calls, got %d", tc.expectedTools, toolCalls)
			}
		})
	}
}

func TestGemma4StreamLexer_XMLThoughtTagsAndResonanceCascadePrevention(t *testing.T) {
	// Tests the pseudo-thought XML tags (<thought>...</thought>) that cause resonance cascades
	input := "<thought>\nI must find where ContextShaper lives.\n</thought>I will now list the directory.\n" +
		`<|tool_call>call:list_directory{path:<|"|>internal/engine<|"|>}<tool_call|>`

	lexer := wire.NewGemma4StreamLexer()
	events := lexer.Feed(input)
	closeEvents, err := lexer.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	events = append(events, closeEvents...)

	var thoughtParts, textParts []string
	var toolCalls []*domain.ToolCall

	for _, ev := range events {
		switch ev.Kind {
		case wire.EventThoughtDelta:
			thoughtParts = append(thoughtParts, ev.Text)
		case wire.EventTextDelta:
			textParts = append(textParts, ev.Text)
		case wire.EventToolCall:
			toolCalls = append(toolCalls, ev.ToolCall)
		}
	}

	fullThought := strings.Join(thoughtParts, "")
	fullText := strings.Join(textParts, "")

	expectedThought := "I must find where ContextShaper lives.\n"
	if fullThought != expectedThought {
		t.Errorf("expected thought %q, got %q", expectedThought, fullThought)
	}

	expectedText := "I will now list the directory.\n"
	if fullText != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, fullText)
	}

	if len(toolCalls) != 1 || toolCalls[0].Function.Name != "list_directory" {
		t.Fatalf("expected tool call list_directory, got %+v", toolCalls)
	}
}

func TestGemma4StreamLexer_StrayCloserAndDelimiterLeakPrevention(t *testing.T) {
	tests := []struct {
		name          string
		chunks        []string
		expectedText  string
		expectedCall  string
		expectedThgt  string
	}{
		{
			name: "Leading channel closer immediately before tool call",
			chunks: []string{
				"<channel|>",
				"<|tool_call>call:read_file{path:<|\"|>internal/engine/shaper.go<|\"|>}<tool_call|>",
			},
			expectedText: "",
			expectedCall: "read_file",
		},
		{
			name: "Leading channel closer with stray closing bracket before tool call",
			chunks: []string{
				"<channel|>>\n",
				"<|tool_call>call:grep_search{pattern:<|\"|>projectPathToMessages<|\"|>}<tool_call|>",
			},
			expectedText: "",
			expectedCall: "grep_search",
		},
		{
			name: "Orphaned thought header before text response",
			chunks: []string{
				"thought <channel|>",
				"The analysis of the `ContextShaper` reveals a mechanism...",
			},
			expectedText: "The analysis of the `ContextShaper` reveals a mechanism...",
		},
		{
			name: "Repeated tool_response closer before text response",
			chunks: []string{
				"<tool_response|>thought <channel|>",
				"The subagent completed its investigation.",
			},
			expectedText: "The subagent completed its investigation.",
		},
		{
			name: "Stray closer on stream close",
			chunks: []string{
				"All tasks completed.",
				"<turn|>",
			},
			expectedText: "All tasks completed.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lexer := wire.NewGemma4StreamLexer()
			var events []wire.StreamEvent
			for _, chunk := range tc.chunks {
				events = append(events, lexer.Feed(chunk)...)
			}
			closeEvents, err := lexer.Close()
			if err != nil {
				t.Fatalf("Close failed: %v", err)
			}
			events = append(events, closeEvents...)

			var textParts, thoughtParts []string
			var toolCalls []*domain.ToolCall
			for _, ev := range events {
				switch ev.Kind {
				case wire.EventTextDelta:
					textParts = append(textParts, ev.Text)
				case wire.EventThoughtDelta:
					thoughtParts = append(thoughtParts, ev.Text)
				case wire.EventToolCall:
					toolCalls = append(toolCalls, ev.ToolCall)
				}
			}

			fullText := strings.Join(textParts, "")
			fullThought := strings.Join(thoughtParts, "")

			if fullText != tc.expectedText {
				t.Errorf("expected text %q, got %q", tc.expectedText, fullText)
			}
			if tc.expectedThgt != "" && fullThought != tc.expectedThgt {
				t.Errorf("expected thought %q, got %q", tc.expectedThgt, fullThought)
			}
			if tc.expectedCall != "" {
				if len(toolCalls) == 0 || toolCalls[0].Function.Name != tc.expectedCall {
					t.Fatalf("expected tool call %s, got %+v", tc.expectedCall, toolCalls)
				}
			} else if len(toolCalls) > 0 {
				t.Fatalf("expected no tool calls, got %+v", toolCalls)
			}
		})
	}
}

func TestGemma4Codec_EncodePrompt_PoisonSanitization(t *testing.T) {
	codec := wire.NewGemma4Codec()

	// Prior assistant node poisoned with leaked control tokens from an older session
	history := []wire.TurnNode{
		{
			Role:    domain.RoleUser,
			Content: "What is ContextShaper?",
		},
		{
			Role:    domain.RoleAssistant,
			Content: "<channel|>>",
			Thought: " \n <thought> \n ",
			ToolCalls: []domain.ToolCall{
				{
					ID: "call_1",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path":"shaper.go"}`),
					},
				},
			},
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "call_1",
			Content:    "package engine...",
		},
	}

	prompt, err := codec.EncodePrompt("You are Gemma.", nil, history)
	if err != nil {
		t.Fatalf("EncodePrompt failed: %v", err)
	}

	// Verify that the poisoned content was sanitized out
	if strings.Contains(prompt, "<channel|>>") || strings.Contains(prompt, "<channel|>") {
		t.Fatalf("poisoned <channel|> leaked into prompt history! Prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "<thought>") || strings.Contains(prompt, "</thought>") {
		t.Fatalf("poisoned <thought> leaked into prompt history! Prompt:\n%s", prompt)
	}
}

func TestGemma4Codec_ReasoningEffortLevels(t *testing.T) {
	tools := []wire.ToolDefinition{
		{
			Name:        "test_tool",
			Description: "A test tool",
		},
	}

	historyWithThought := []wire.TurnNode{
		{
			Role:    domain.RoleUser,
			Content: "Solve this",
		},
		{
			Role:    domain.RoleAssistant,
			Thought: "Intra-turn working thought",
			ToolCalls: []domain.ToolCall{
				{
					ID: "c1",
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      "test_tool",
						Arguments: json.RawMessage(`{}`),
					},
				},
			},
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "c1",
			Content:    "result 1",
		},
	}

	// 1. Level: "off"
	codecOff := wire.NewGemma4Codec(wire.WithReasoningEffort("off"))
	promptOff, err := codecOff.EncodePrompt("You are Gemma.", tools, historyWithThought)
	if err != nil {
		t.Fatalf("EncodePrompt off failed: %v", err)
	}
	if strings.Contains(promptOff, "<|think|>") {
		t.Errorf("expected no <|think|> token in system block under off, got:\n%s", promptOff)
	}
	if !strings.Contains(promptOff, "Output no internal thought") {
		t.Errorf("expected off rubric in prompt, got:\n%s", promptOff)
	}
	if strings.Contains(promptOff, "Intra-turn working thought") {
		t.Errorf("expected intra-turn thought omitted under off, got:\n%s", promptOff)
	}
	expectedOffSuffix := "<tool_response|><|channel>thought\n<channel|>"
	if !strings.HasSuffix(promptOff, expectedOffSuffix) {
		t.Errorf("expected prompt to end with %q under off, got: %q", expectedOffSuffix, promptOff)
	}

	// 2. Level: "low"
	codecLow := wire.NewGemma4Codec(wire.WithReasoningEffort("low"))
	promptLow, err := codecLow.EncodePrompt("You are Gemma.", tools, historyWithThought)
	if err != nil {
		t.Fatalf("EncodePrompt low failed: %v", err)
	}
	if !strings.HasPrefix(promptLow, "<|turn>system\n<|think|>\nOperating Guidelines:") {
		t.Errorf("expected <|think|> followed by Operating Guidelines at start of system block under low, got:\n%s", promptLow)
	}
	if !strings.Contains(promptLow, "<tool|>\nYou are Gemma.\n<turn|>") {
		t.Errorf("expected persona at end of system block immediately before <turn|> under low, got:\n%s", promptLow)
	}
	if !strings.Contains(promptLow, "Keep thoughts concise") {
		t.Errorf("expected low scratchpad rubric in prompt, got:\n%s", promptLow)
	}
	if !strings.Contains(promptLow, "Intra-turn working thought") {
		t.Errorf("expected intra-turn thought preserved under low, got:\n%s", promptLow)
	}
	if !strings.HasSuffix(promptLow, "<tool_response|>") {
		t.Errorf("expected prompt to end with <tool_response|> under low, got: %q", promptLow)
	}

	// 3. Level: "medium"
	codecMed := wire.NewGemma4Codec(wire.WithReasoningEffort("medium"))
	promptMed, err := codecMed.EncodePrompt("You are Gemma.", tools, historyWithThought)
	if err != nil {
		t.Fatalf("EncodePrompt med failed: %v", err)
	}
	if !strings.HasPrefix(promptMed, "<|turn>system\n<|think|>\nOperating Guidelines:") {
		t.Errorf("expected <|think|> followed by Operating Guidelines at start of system block under medium, got:\n%s", promptMed)
	}
	if !strings.Contains(promptMed, "<tool|>\nYou are Gemma.\n<turn|>") {
		t.Errorf("expected persona at end of system block immediately before <turn|> under medium, got:\n%s", promptMed)
	}
	if !strings.Contains(promptMed, "Analyze the task step by step") {
		t.Errorf("expected medium rubric in prompt, got:\n%s", promptMed)
	}
	if !strings.HasSuffix(promptMed, "<tool_response|>") {
		t.Errorf("expected prompt to end with <tool_response|> under medium, got: %q", promptMed)
	}

	// 4. Level: "high" (Channel Forcing)
	codecHigh := wire.NewGemma4Codec(wire.WithReasoningEffort("high"))
	promptHigh, err := codecHigh.EncodePrompt("You are Gemma.", tools, historyWithThought)
	if err != nil {
		t.Fatalf("EncodePrompt high failed: %v", err)
	}
	if !strings.HasPrefix(promptHigh, "<|turn>system\n<|think|>\nOperating Guidelines:") {
		t.Errorf("expected <|think|> followed by Operating Guidelines at start of system block under high, got:\n%s", promptHigh)
	}
	if !strings.Contains(promptHigh, "<tool|>\nYou are Gemma.\n<turn|>") {
		t.Errorf("expected persona at end of system block immediately before <turn|> under high, got:\n%s", promptHigh)
	}
	if !strings.Contains(promptHigh, "Deep reasoning: Engage in rigorous") {
		t.Errorf("expected high deep reasoning rubric in prompt, got:\n%s", promptHigh)
	}
	expectedHighSuffix := "<tool_response|><|channel>thought\n"
	if !strings.HasSuffix(promptHigh, expectedHighSuffix) {
		t.Fatalf("expected channel-forcing suffix %q under high, got:\n%s", expectedHighSuffix, promptHigh)
	}

	// 5. Test Lexer starts in stateInsideThought when ReasoningEffort is "high"
	lexerHigh := codecHigh.NewStreamLexer()
	events := lexerHigh.Feed("Deep analytical deliberation about the architecture.\n<channel|><|tool_call>call:test_tool{}<tool_call|>")
	closeEvents, err := lexerHigh.Close()
	if err != nil {
		t.Fatalf("lexer Close failed: %v", err)
	}
	events = append(events, closeEvents...)

	var thoughtText, regularText string
	var callCount int
	for _, ev := range events {
		switch ev.Kind {
		case wire.EventThoughtDelta:
			thoughtText += ev.Text
		case wire.EventTextDelta:
			regularText += ev.Text
		case wire.EventToolCall:
			callCount++
		}
	}

	if regularText != "" {
		t.Errorf("expected zero regular text from channel-forced thought, got: %q", regularText)
	}
	if !strings.Contains(thoughtText, "Deep analytical deliberation") {
		t.Errorf("expected thought delta to contain deliberation, got: %q", thoughtText)
	}
	if callCount != 1 {
		t.Errorf("expected 1 tool call, got %d", callCount)
	}

	// 6. Regression test: when lexer starts in stateInsideThought (high effort) and model
	// still emits a redundant "<|channel>thought\n<channel|>>" before text or tool calls,
	// neither "thought\n" nor leading ">" should leak into Thought or Content.
	lexerHighEcho := codecHigh.NewStreamLexer()
	var echoEvents []wire.StreamEvent
	for _, ch := range []string{"<|channel>", "thought\n", "<channel|>", ">>>>The Grand Codex is ready."} {
		echoEvents = append(echoEvents, lexerHighEcho.Feed(ch)...)
	}
	closeEcho, err := lexerHighEcho.Close()
	if err != nil {
		t.Fatalf("lexer Close failed: %v", err)
	}
	echoEvents = append(echoEvents, closeEcho...)

	var echoThought, echoText string
	for _, ev := range echoEvents {
		switch ev.Kind {
		case wire.EventThoughtDelta:
			echoThought += ev.Text
		case wire.EventTextDelta:
			echoText += ev.Text
		}
	}
	if echoThought != "" {
		t.Errorf("expected empty thought when model echoes empty <|channel>thought\\n<channel|>, got %q", echoThought)
	}
	if echoText != "The Grand Codex is ready." {
		t.Errorf("expected clean text without leading '>>>>', got %q", echoText)
	}

	// 7. Regression test: when lexer starts in stateInsideThought (high effort) and model
	// echoes a bare "thought\n" before actual reasoning tokens, strip the leading "thought\n".
	lexerBareEcho := codecHigh.NewStreamLexer()
	var bareEvents []wire.StreamEvent
	for _, ch := range []string{"thou", "ght\nThe user greeted me.\n<channel|>", "Good morning!"} {
		bareEvents = append(bareEvents, lexerBareEcho.Feed(ch)...)
	}
	closeBare, err := lexerBareEcho.Close()
	if err != nil {
		t.Fatalf("lexer Close failed: %v", err)
	}
	bareEvents = append(bareEvents, closeBare...)

	var bareThought, bareText string
	for _, ev := range bareEvents {
		switch ev.Kind {
		case wire.EventThoughtDelta:
			bareThought += ev.Text
		case wire.EventTextDelta:
			bareText += ev.Text
		}
	}
	if bareThought != "The user greeted me.\n" {
		t.Errorf("expected bare 'thought\\n' stripped from thought delta, got %q", bareThought)
	}
	if bareText != "Good morning!" {
		t.Errorf("expected clean text 'Good morning!', got %q", bareText)
	}
}


