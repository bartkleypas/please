package domain

import (
	"encoding/json"
	"testing"
)

func TestRoleConstants(t *testing.T) {
	roles := []Role{RoleSystem, RoleUser, RoleAssistant, RoleTool, RoleSummary}
	expected := []string{"system", "user", "assistant", "tool", "summary"}

	for i, r := range roles {
		if string(r) != expected[i] {
			t.Errorf("expected role %s, got %s", expected[i], r)
		}
	}
}

func TestToolCategoryConstants(t *testing.T) {
	cats := []ToolCategory{CategorySensory, CategoryMutate, CategoryExecute}
	expected := []string{"sensory", "mutate", "execute"}

	for i, c := range cats {
		if string(c) != expected[i] {
			t.Errorf("expected category %s, got %s", expected[i], c)
		}
	}
}

func TestSandboxPolicyConstants(t *testing.T) {
	policies := []SandboxPolicy{SandboxPolicyStrict, SandboxPolicyStandard, SandboxPolicyPermissive}
	expected := []string{"strict", "standard", "permissive"}

	for i, p := range policies {
		if string(p) != expected[i] {
			t.Errorf("expected policy %s, got %s", expected[i], p)
		}
	}
}

func TestMessageSerialization(t *testing.T) {
	msg := Message{
		ID:       "msg-123",
		ParentID: "msg-000",
		Role:     RoleAssistant,
		Content:  "Hello world",
		Thought:  "Thinking...",
		ToolCalls: []ToolCall{
			{
				ID:   "call-1",
				Type: "function",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"main.go"}`),
				},
			},
		},
		Observations: []ToolObservation{
			{
				ToolCallID: "call-1",
				Result:     "file contents",
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal Message: %v", err)
	}

	var roundTrip Message
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal Message: %v", err)
	}

	if roundTrip.ID != msg.ID {
		t.Errorf("expected ID %s, got %s", msg.ID, roundTrip.ID)
	}
	if roundTrip.Role != RoleAssistant {
		t.Errorf("expected RoleAssistant, got %s", roundTrip.Role)
	}
	if len(roundTrip.ToolCalls) != 1 || roundTrip.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("tool call mismatch in round trip: %+v", roundTrip.ToolCalls)
	}
	if len(roundTrip.Observations) != 1 || roundTrip.Observations[0].Result != "file contents" {
		t.Errorf("observation mismatch in round trip: %+v", roundTrip.Observations)
	}
}

func TestModelOptionsSerialization(t *testing.T) {
	temp := 0.7
	maxTok := 2048
	opts := ModelOptions{
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	data, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("failed to marshal ModelOptions: %v", err)
	}

	var roundTrip ModelOptions
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal ModelOptions: %v", err)
	}

	if roundTrip.Temperature == nil || *roundTrip.Temperature != 0.7 {
		t.Errorf("expected Temperature 0.7, got %v", roundTrip.Temperature)
	}
	if roundTrip.MaxTokens == nil || *roundTrip.MaxTokens != 2048 {
		t.Errorf("expected MaxTokens 2048, got %v", roundTrip.MaxTokens)
	}
	if roundTrip.TopP != nil {
		t.Errorf("expected nil TopP, got %v", roundTrip.TopP)
	}
}

func TestToolSpecSerialization(t *testing.T) {
	spec := ToolSpec{
		Name:        "execute_command",
		Category:    CategoryExecute,
		Description: "Run shell command",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string"},
			},
		},
		Interactive: true,
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("failed to marshal ToolSpec: %v", err)
	}

	var roundTrip ToolSpec
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal ToolSpec: %v", err)
	}

	if roundTrip.Name != "execute_command" {
		t.Errorf("expected name execute_command, got %s", roundTrip.Name)
	}
	if roundTrip.Category != CategoryExecute {
		t.Errorf("expected category execute, got %s", roundTrip.Category)
	}
	if !roundTrip.Interactive {
		t.Errorf("expected Interactive to be true")
	}
}
