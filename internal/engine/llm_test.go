package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func setupLiveFire(t *testing.T) (*Manager, LLMProvider) {
	if os.Getenv("PLEASE_LIVE_FIRE") == "" {
		t.Skip("Skipping live fire testing (set PLEASE_LIVE_FIRE=1 to run)")
	}

	// Change working directory to project root so tools using relative paths work
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("failed to change directory to project root: %v", err)
	}

	// Create test vault dir in the project root
	testVaultDir := "test_vault"
	err := os.MkdirAll(testVaultDir, 0755)
	if err != nil {
		t.Fatalf("failed to create test_vault dir: %v", err)
	}
	dbPath := filepath.Join(testVaultDir, "livefire.db")

	// Delete existing DB to start fresh
	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	storage, err := NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to initialize sqlite storage: %v", err)
	}

	graph, _, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	mgr := NewManager(graph, storage)
	mgr.RegisterDefaultTools()

	// 1. Default configuration
	endpoint := "http://localhost:11434/api/chat"
	model := "gemma4:e4b"

	providerType := "ollama"
	apiKey := ""

	var options *ModelOptions

	// 2. Try loading a workspace-specific test config
	workspaceConfigPath := "livefire.json"
	if data, err := os.ReadFile(workspaceConfigPath); err == nil {
		var wsConfig struct {
			Provider string        `json:"provider"`
			Endpoint string        `json:"endpoint"`
			Model    string        `json:"model"`
			APIKey   string        `json:"api_key"`
			Options  *ModelOptions `json:"options"`
		}
		if json.Unmarshal(data, &wsConfig) == nil {
			if wsConfig.Provider != "" {
				providerType = wsConfig.Provider
			}
			if wsConfig.Endpoint != "" {
				endpoint = wsConfig.Endpoint
			}
			if wsConfig.Model != "" {
				model = wsConfig.Model
			}
			if wsConfig.APIKey != "" {
				apiKey = wsConfig.APIKey
			}
			if wsConfig.Options != nil {
				options = wsConfig.Options
			}
		}
	} else {
		// 3. Fall back to the user's global please config
		if globalCfg, err := LoadConfig(); err == nil && globalCfg.Server != nil {
			if globalCfg.Server.Provider != "" {
				providerType = globalCfg.Server.Provider
			}
			if globalCfg.Server.Endpoint != "" {
				endpoint = globalCfg.Server.Endpoint
			}
			if globalCfg.Server.Model != "" {
				model = globalCfg.Server.Model
			}
			if globalCfg.Server.APIKey != "" {
				apiKey = globalCfg.Server.APIKey
			}
			if globalCfg.Server.Options != nil {
				options = globalCfg.Server.Options
			}
		}
	}

	// 4. Environment variables can override everything
	if envProvider := os.Getenv("PLEASE_PROVIDER"); envProvider != "" {
		providerType = envProvider
	}
	if envModel := os.Getenv("OLLAMA_MODEL"); envModel != "" {
		model = envModel
	}
	if envEndpoint := os.Getenv("OLLAMA_ENDPOINT"); envEndpoint != "" {
		endpoint = envEndpoint
	}
	if envAPIKey := os.Getenv("OPENAI_API_KEY"); envAPIKey != "" {
		apiKey = envAPIKey
	}

	var provider LLMProvider
	if providerType == "openai" {
		provider = NewOpenAIProvider(endpoint, model, apiKey, options)
	} else {
		provider = NewOllamaProvider(endpoint, model, options)
	}

	return mgr, provider
}

func simulateTurn(t *testing.T, ctx context.Context, mgr *Manager, provider LLMProvider, input string, parentID string) *Node {
	userNode, err := mgr.CreateNode(parentID, RoleUser, input, false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}
	t.Logf("### [Node ID: %s] User says: %s", userNode.ID, input)

	messages, err := mgr.BuildLLMContext(userNode.ID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	assistantNode, err := mgr.CreateAssistantNode(userNode.ID, resp.Content, resp.Thought, resp.ToolCalls, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}
	t.Logf("### [Node ID: %s] Assistant says: %s", assistantNode.ID, resp.Content)

	return assistantNode
}

func executeToolTurn(t *testing.T, ctx context.Context, mgr *Manager, provider LLMProvider, tools []Tool, input string, parentID string) *Node {
	userNode, err := mgr.CreateNode(parentID, RoleUser, input, false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}
	t.Logf("### [Node ID: %s] User says: %s", userNode.ID, input)

	messages, err := mgr.BuildLLMContext(userNode.ID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	resp, err := provider.GenerateResponse(ctx, messages, tools)
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	toolCallsJSON, _ := json.Marshal(resp.ToolCalls)
	if len(resp.ToolCalls) == 0 {
		t.Fatalf("expected tool calls, got none. Model responded with: %s", resp.Content)
	}

	assistantNode, err := mgr.CreateAssistantNode(userNode.ID, resp.Content, resp.Thought, resp.ToolCalls, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}
	t.Logf("### [Node ID: %s] Assistant emitted tool calls: %s", assistantNode.ID, string(toolCallsJSON))

	maxLoops := 5
	loopCount := 0
	currentResp := resp

	for len(currentResp.ToolCalls) > 0 && loopCount < maxLoops {
		loopCount++

		for _, call := range currentResp.ToolCalls {
			t.Logf("### Executing tool: %s(%s)", call.Function.Name, string(call.Function.Arguments))
			result, err := mgr.ExecuteToolCall(ctx, call)
			if err != nil {
				t.Logf("### Tool Error: %v", err)
				result = fmt.Sprintf("Error: %v", err)
			}

			previewResult := result
			if len(previewResult) > 200 {
				previewResult = previewResult[:180] + fmt.Sprintf("... [total %d bytes]", len(result))
			}
			previewResult = strings.ReplaceAll(previewResult, "\n", " ")
			t.Logf("### Tool Result (%s): %s", call.Function.Name, previewResult)

			callID := call.ID
			if callID == "" {
				callID = "call_" + call.Function.Name
			}

			err = mgr.UpdateAssistantObservations(assistantNode.ID, callID, result)
			if err != nil {
				t.Fatalf("failed to update observations: %v", err)
			}
		}

		// Rebuild context to include new observations on the active node
		messages, err = mgr.BuildLLMContext(assistantNode.ID, false)
		if err != nil {
			t.Fatalf("failed to build LLM context: %v", err)
		}

		t.Logf("### Requesting follow-up from Assistant (Loop %d)", loopCount)
		currentResp, err = provider.GenerateResponse(ctx, messages, tools)
		if err != nil {
			t.Fatalf("failed to generate follow-up response: %v", err)
		}

		if currentResp.Thought != "" {
			thoughtPreview := strings.ReplaceAll(currentResp.Thought, "\n", " ")
			if len(thoughtPreview) > 150 {
				thoughtPreview = thoughtPreview[:140] + "..."
			}
			t.Logf("### Assistant thought: %s", thoughtPreview)
		}
		t.Logf("### Assistant follow-up says: %s", currentResp.Content)
		if len(currentResp.ToolCalls) > 0 {
			followUpJSON, _ := json.Marshal(currentResp.ToolCalls)
			t.Logf("### Assistant emitted follow-up tool calls: %s", string(followUpJSON))
		}

		// Append to the EXISTING assistant node (matching TUI behavior)
		assistantNode, _ = mgr.GetNode(assistantNode.ID)
		assistantNode.Content += currentResp.Content
		assistantNode.Thought += currentResp.Thought
		assistantNode.ToolCalls = append(assistantNode.ToolCalls, currentResp.ToolCalls...)
		if err := mgr.Storage.SaveNode(assistantNode); err != nil {
			t.Fatalf("failed to update assistant node: %v", err)
		}
	}

	if loopCount == maxLoops {
		t.Logf("### Warning: Max tool loops reached")
	}

	return assistantNode
}

func TestLLM_Narrator(t *testing.T) {
	mgr, provider := setupLiveFire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Establish the System prompt
	sysNode, err := mgr.CreateNode("", RoleSystem, "You are a helpful and concise narrator.", false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	// 2. Run a simple handshake
	simulateTurn(t, ctx, mgr, provider, "Hello! Can you confirm you are online?", sysNode.ID)
}

func TestLLM_ToolExecution(t *testing.T) {
	mgr, provider := setupLiveFire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Establish the System prompt
	sysNode, err := mgr.CreateNode("", RoleSystem, "You are a helpful assistant with access to tools. Use tools when asked.", false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	// 2. Extract tools from registry to pass to provider
	var tools []Tool
	for _, tool := range mgr.Registry.Tools {
		tools = append(tools, tool)
	}

	// 3. Ask it to read a file
	input := "Please use the execute_command tool to run 'date' and tell me the result."
	executeToolTurn(t, ctx, mgr, provider, tools, input, sysNode.ID)

	// 4. Ask it to read a file, but don't provide the exact name.
	// This should force the model to first try to find it with the file search tool (loop 1), then use the read tool against what it finds and summarize it (loop 2).
	input = "Please look in the `Lore/` directory. There should be a file about a garden in there. Would you please summarize that file for me?"
	executeToolTurn(t, ctx, mgr, provider, tools, input, sysNode.ID)

	// 5. Chain of tool executions to test all file system tools
	t.Log("--- Starting File System Tools Chain ---")

	// write_file
	input = "Please use the write_file tool to create a new file at 'test_vault/demo.txt' with the exact content:\nLine 1\nLine 2\nLine 3\n"
	turn1 := executeToolTurn(t, ctx, mgr, provider, tools, input, sysNode.ID)

	// list_directory
	input = "Please use the list_directory tool to list the contents of the 'test_vault' directory to verify the file was created."
	turn2 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn1.ID)

	// list_files_recursive
	input = "Please use the list_files_recursive tool to list all files in the 'test_vault' directory."
	turn3 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn2.ID)

	// grep_search
	input = "Please use the grep_search tool to search for the pattern 'Line 2' in the `.txt` files within the 'test_vault/' directory."
	turn4 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn3.ID)

	// patch_file
	input = "Please use the patch_file tool to replace the exact string 'Line 2' with 'Line Two' in 'test_vault/demo.txt'."
	turn5 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn4.ID)

	// edit_file
	input = "Please use the edit_file tool in 'replace_line' mode to replace line 3 of 'test_vault/demo.txt' with 'Line Three'."
	turn6 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn5.ID)

	// search_and_replace
	input = "Please use the search_and_replace tool to replace the exact block 'Line 1' with 'Line One' in 'test_vault/demo.txt'."
	turn7 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn6.ID)

	// cleanup
	input = "Please use the execute_command tool to run 'rm test_vault/demo.txt'."
	turn8 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn7.ID)

	// Feedback
	input = "We just tested write_file, list_directory, list_files_recursive, grep_search, patch_file, edit_file, search_and_replace, and execute_command. Summarize your experience with these tools and their ease of use."
	simulateTurn(t, ctx, mgr, provider, input, turn8.ID)
}

func executeAutonomousTurn(t *testing.T, ctx context.Context, mgr *Manager, provider LLMProvider, tools []Tool, input string, parentID string) *Node {
	userNode, err := mgr.CreateNode(parentID, RoleUser, input, false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}
	t.Logf("### [Node ID: %s] User says: %s", userNode.ID, input)

	messages, err := mgr.BuildLLMContext(userNode.ID, false)
	if err != nil {
		t.Fatalf("failed to build LLM context: %v", err)
	}

	resp, err := provider.GenerateResponse(ctx, messages, tools)
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	assistantNode, err := mgr.CreateAssistantNode(userNode.ID, resp.Content, resp.Thought, resp.ToolCalls, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}

	if resp.Thought != "" {
		thoughtPreview := strings.ReplaceAll(resp.Thought, "\n", " ")
		if len(thoughtPreview) > 150 {
			thoughtPreview = thoughtPreview[:140] + "..."
		}
		t.Logf("### Assistant thought: %s", thoughtPreview)
	}
	if resp.Content != "" {
		t.Logf("### [Node ID: %s] Assistant says: %s", assistantNode.ID, resp.Content)
	}

	maxLoops := 3
	loopCount := 0
	currentResp := resp

	for len(currentResp.ToolCalls) > 0 && loopCount < maxLoops {
		loopCount++

		for _, call := range currentResp.ToolCalls {
			t.Logf("### Executing tool: %s(%s)", call.Function.Name, string(call.Function.Arguments))
			result, err := mgr.ExecuteToolCall(ctx, call)
			if err != nil {
				t.Logf("### Tool Error: %v", err)
				result = fmt.Sprintf("Error: %v", err)
			}

			previewResult := result
			if len(previewResult) > 200 {
				previewResult = previewResult[:180] + fmt.Sprintf("... [total %d bytes]", len(result))
			}
			previewResult = strings.ReplaceAll(previewResult, "\n", " ")
			t.Logf("### Tool Result (%s): %s", call.Function.Name, previewResult)

			callID := call.ID
			if callID == "" {
				callID = "call_" + call.Function.Name
			}

			err = mgr.UpdateAssistantObservations(assistantNode.ID, callID, result)
			if err != nil {
				t.Fatalf("failed to update observations: %v", err)
			}
		}

		// Rebuild context to include new observations on the active node
		messages, err = mgr.BuildLLMContext(assistantNode.ID, false)
		if err != nil {
			t.Fatalf("failed to build LLM context: %v", err)
		}

		t.Logf("### Requesting follow-up from Assistant (Loop %d)", loopCount)
		currentResp, err = provider.GenerateResponse(ctx, messages, tools)
		if err != nil {
			t.Fatalf("failed to generate follow-up response: %v", err)
		}

		if currentResp.Thought != "" {
			thoughtPreview := strings.ReplaceAll(currentResp.Thought, "\n", " ")
			if len(thoughtPreview) > 150 {
				thoughtPreview = thoughtPreview[:140] + "..."
			}
			t.Logf("### Assistant thought: %s", thoughtPreview)
		}
		if currentResp.Content != "" {
			t.Logf("### Assistant follow-up says: %s", currentResp.Content)
		}

		// Append to the assistant node
		assistantNode, _ = mgr.GetNode(assistantNode.ID)
		assistantNode.Content += currentResp.Content
		assistantNode.Thought += currentResp.Thought
		assistantNode.ToolCalls = append(assistantNode.ToolCalls, currentResp.ToolCalls...)
		if err := mgr.Storage.SaveNode(assistantNode); err != nil {
			t.Fatalf("failed to update assistant node: %v", err)
		}
	}

	return assistantNode
}

func TestLLM_AutonomousNarrativeVector(t *testing.T) {
	mgr, provider := setupLiveFire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// 1. Establish George the Archivist Persona
	sysPrompt := "You are George the Archivist 🦉📚, an 18-inch tall barred owl who chronicles software architecture. You speak with a sardonic wit and deep appreciation for structured DAGs. To anchor your trajectory in the map, conclude the text of each turn with a 1-3 emoji signature (signat) on the final line reflecting your active posture (e.g. 🔍📜 for reading files, 🛠️💻 for code analysis, 🧠📐 for architecture logic)."
	sysNode, err := mgr.CreateNode("", RoleSystem, sysPrompt, false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	var tools []Tool
	for _, tool := range mgr.Registry.Tools {
		tools = append(tools, tool)
	}

	// 2. Mission Primer
	primerPrompt := "Greetings George! Over the next 5 turns, explore this workspace autonomously. On each turn, use the read_file tool to inspect a different core file (e.g. README.md, context_resonance.md, GEMINI.md, internal/engine/service.go, internal/tui/map.go) and weave your findings into the lore of Please. Conclude each message with your 1-3 emoji signat on the final line. Your continuation prompt will always be simply: 'Please proceed.'"
	primerTurn := executeAutonomousTurn(t, ctx, mgr, provider, tools, primerPrompt, sysNode.ID)

	var turnIDs []string
	turnIDs = append(turnIDs, primerTurn.ParentID, primerTurn.ID)
	currParentID := primerTurn.ID

	// 3. 4 more autonomous turns fueled only by "Please proceed."
	for i := 2; i <= 5; i++ {
		t.Logf("=== Starting Autonomous Vector Turn %d/5 ===", i)
		asstTurn := executeAutonomousTurn(t, ctx, mgr, provider, tools, "Please proceed.", currParentID)
		turnIDs = append(turnIDs, asstTurn.ParentID, asstTurn.ID)
		currParentID = asstTurn.ID

		t.Logf("Turn %d completed (Node: %s, Signat: %q)", i, asstTurn.ID, asstTurn.Metadata["signat"])
	}

	// 4. Synthesize 5-Turn Milestone Compaction
	t.Log("=== Compacting Autonomous Vector into Supernode ===")
	superNode, err := mgr.CompactRangeWithDirective(
		ctx,
		provider,
		turnIDs,
		"synthesize George's 5-step workspace chronicle into an authoritative architectural milestone",
	)
	if err != nil {
		t.Fatalf("failed to compact autonomous vector range: %v", err)
	}

	t.Logf("### Generated Supernode (ID: %s):\n%s", superNode.ID, superNode.Content)

	if superNode.Role != RoleSummary {
		t.Errorf("expected supernode to have RoleSummary, got %s", superNode.Role)
	}
	if superNode.ParentID != sysNode.ID {
		t.Errorf("expected supernode ParentID to be system root (%s), got: %s", sysNode.ID, superNode.ParentID)
	}
	if !strings.Contains(superNode.Content, "🎯 Trajectory:") {
		t.Errorf("expected supernode to contain '🎯 Trajectory:' header, got:\n%s", superNode.Content)
	}
}

func TestOllamaProvider_OptionsSerialization(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		resp := ollamaResponse{
			Message: ollamaMessage{
				Role:    "assistant",
				Content: "Hello from mock ollama",
			},
			Done: true,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	temp := 0.7
	topP := 0.9
	topK := 40
	minP := 0.05
	numCtx := 16384
	maxTokens := 2048
	repeatPenalty := 1.1
	repeatLastN := 128
	freqPenalty := 0.15

	options := &ModelOptions{
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topK,
		MinP:             &minP,
		NumCtx:           &numCtx,
		MaxTokens:        &maxTokens,
		RepeatPenalty:    &repeatPenalty,
		RepeatLastN:      &repeatLastN,
		FrequencyPenalty: &freqPenalty,
	}

	provider := NewOllamaProvider(server.URL, "test-model", options)
	ctx := context.Background()
	messages := []Message{{Role: RoleUser, Content: "hi"}}
	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if resp.Content != "Hello from mock ollama" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal(capturedBody, &reqData); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	opts, ok := reqData["options"].(map[string]interface{})
	if !ok || opts == nil {
		t.Fatalf("expected options object in request body, got: %v", reqData["options"])
	}

	if opts["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", opts["temperature"])
	}
	if opts["top_p"] != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", opts["top_p"])
	}
	if opts["top_k"] != float64(40) {
		t.Errorf("expected top_k 40, got %v", opts["top_k"])
	}
	if opts["min_p"] != 0.05 {
		t.Errorf("expected min_p 0.05, got %v", opts["min_p"])
	}
	if opts["num_ctx"] != float64(16384) {
		t.Errorf("expected num_ctx 16384, got %v", opts["num_ctx"])
	}
	if opts["num_predict"] != float64(2048) {
		t.Errorf("expected num_predict 2048, got %v", opts["num_predict"])
	}
	if opts["repeat_penalty"] != 1.1 {
		t.Errorf("expected repeat_penalty 1.1, got %v", opts["repeat_penalty"])
	}
	if opts["repeat_last_n"] != float64(128) {
		t.Errorf("expected repeat_last_n 128, got %v", opts["repeat_last_n"])
	}
	if opts["frequency_penalty"] != 0.15 {
		t.Errorf("expected frequency_penalty 0.15, got %v", opts["frequency_penalty"])
	}
}

func TestOpenAIProvider_OptionsSerialization(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		resp := openAIResponse{
			Choices: []struct {
				Message      openAIMessage `json:"message"`
				Delta        openAIMessage `json:"delta"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message: openAIMessage{
						Role:    "assistant",
						Content: "Hello from mock openai",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	temp := 0.2
	topP := 0.85
	maxTokens := 1024
	freqPenalty := 0.25

	options := &ModelOptions{
		Temperature:      &temp,
		TopP:             &topP,
		MaxTokens:        &maxTokens,
		FrequencyPenalty: &freqPenalty,
	}

	provider := NewOpenAIProvider(server.URL, "gpt-4o", "test-key", options)
	ctx := context.Background()
	messages := []Message{{Role: RoleUser, Content: "hi"}}
	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if resp.Content != "Hello from mock openai" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal(capturedBody, &reqData); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	if reqData["temperature"] != 0.2 {
		t.Errorf("expected temperature 0.2, got %v", reqData["temperature"])
	}
	if reqData["top_p"] != 0.85 {
		t.Errorf("expected top_p 0.85, got %v", reqData["top_p"])
	}
	if reqData["max_tokens"] != float64(1024) {
		t.Errorf("expected max_tokens 1024, got %v", reqData["max_tokens"])
	}
	if reqData["frequency_penalty"] != 0.25 {
		t.Errorf("expected frequency_penalty 0.25, got %v", reqData["frequency_penalty"])
	}
}

func TestConfig_OptionsSerialization(t *testing.T) {
	temp := 0.5
	ctxVal := 8192
	cfg := &Config{
		Server: &ServerConfig{
			Provider:    "ollama",
			Model:       "llama3:8b",
			Endpoint:    "http://localhost:11434/api/chat",
			VaultPath:   "vault.db",
			StorageType: "sqlite",
			Options: &ModelOptions{
				Temperature: &temp,
				NumCtx:      &ctxVal,
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if loaded.Server == nil || loaded.Server.Options == nil {
		t.Fatalf("expected server options to be non-nil")
	}
	if loaded.Server.Options.Temperature == nil || *loaded.Server.Options.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %v", loaded.Server.Options.Temperature)
	}
	if loaded.Server.Options.NumCtx == nil || *loaded.Server.Options.NumCtx != 8192 {
		t.Errorf("expected num_ctx 8192, got %v", loaded.Server.Options.NumCtx)
	}
	if loaded.Server.Options.TopP != nil {
		t.Errorf("expected top_p to be nil, got %v", loaded.Server.Options.TopP)
	}
}

func TestConfig_SaveAndLoad_Isolation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PLEASE_CONFIG_DIR", tmpDir)

	temp := 0.6
	cfg := &Config{
		Server: &ServerConfig{
			Provider:    "openai",
			Model:       "gpt-4o-mini",
			Endpoint:    "https://api.openai.com/v1/chat/completions",
			VaultPath:   filepath.Join(tmpDir, "vault.db"),
			StorageType: "sqlite",
			Options: &ModelOptions{
				Temperature: &temp,
			},
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config to isolated dir: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config from isolated dir: %v", err)
	}

	if loaded.Server == nil || loaded.Server.Model != "gpt-4o-mini" {
		t.Errorf("expected model gpt-4o-mini, got %v", loaded.Server)
	}
	if loaded.Server.Options == nil || loaded.Server.Options.Temperature == nil || *loaded.Server.Options.Temperature != 0.6 {
		t.Errorf("expected temperature 0.6, got %v", loaded.Server.Options)
	}
}

func TestOpenAIProvider_ReasoningExtraction(t *testing.T) {
	// 1. Test Batch mode with reasoning_content
	serverBatch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := openAIResponse{
			Choices: []struct {
				Message      openAIMessage `json:"message"`
				Delta        openAIMessage `json:"delta"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message: openAIMessage{
						Role:             "assistant",
						Content:          "Final answer",
						ReasoningContent: "Thinking step 1... step 2...",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer serverBatch.Close()

	providerBatch := NewOpenAIProvider(serverBatch.URL, "deepseek-r1", "key", nil)
	msg, err := providerBatch.GenerateResponse(context.Background(), []Message{{Role: RoleUser, Content: "problem"}}, nil)
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if msg.Content != "Final answer" {
		t.Errorf("expected content 'Final answer', got '%s'", msg.Content)
	}
	if msg.Thought != "Thinking step 1... step 2..." {
		t.Errorf("expected thought 'Thinking step 1... step 2...', got '%s'", msg.Thought)
	}

	// 2. Test Streaming mode with reasoning_content deltas
	serverStream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		// Chunk 1: Thought delta
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Thought chunk \"}}]}\n\n")
		flusher.Flush()

		// Chunk 2: Content delta
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Answer chunk\"}}]}\n\n")
		flusher.Flush()

		// Done
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer serverStream.Close()

	providerStream := NewOpenAIProvider(serverStream.URL, "deepseek-r1", "key", nil)
	contentChan, thoughtChan, _, errChan := providerStream.GenerateResponseStream(context.Background(), []Message{{Role: RoleUser, Content: "problem"}}, nil)

	var receivedContent string
	var receivedThought string

	for {
		select {
		case c, ok := <-contentChan:
			if ok {
				receivedContent += c
			}
		case th, ok := <-thoughtChan:
			if ok {
				receivedThought += th
			}
		case err, ok := <-errChan:
			if ok && err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
			goto Done
		}
		if contentChan == nil && thoughtChan == nil {
			break
		}
	}
Done:
	if receivedThought != "Thought chunk " {
		t.Errorf("expected stream thought 'Thought chunk ', got '%s'", receivedThought)
	}
	if receivedContent != "Answer chunk" {
		t.Errorf("expected stream content 'Answer chunk', got '%s'", receivedContent)
	}
}

func TestConfig_WorkspaceDir(t *testing.T) {
	// 1. Unset returns "."
	var emptyCfg Config
	if emptyCfg.GetWorkspaceDir() != "." {
		t.Errorf("expected empty WorkspaceDir to return '.', got %s", emptyCfg.GetWorkspaceDir())
	}

	// 2. Custom path returns absolute path
	tmpDir := t.TempDir()
	cfg := Config{Server: &ServerConfig{WorkspaceDir: tmpDir}}
	absTmp, _ := filepath.Abs(tmpDir)
	if cfg.GetWorkspaceDir() != absTmp {
		t.Errorf("expected %s, got %s", absTmp, cfg.GetWorkspaceDir())
	}

	// 3. Tilde expansion & trailing slash handling
	home, _ := os.UserHomeDir()
	tildeCfg := Config{Server: &ServerConfig{WorkspaceDir: "~/my-project"}}
	expectedTilde := filepath.Join(home, "my-project")
	if tildeCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s, got %s", expectedTilde, tildeCfg.GetWorkspaceDir())
	}

	tildeSlashCfg := Config{Server: &ServerConfig{WorkspaceDir: "~/my-project/"}}
	if tildeSlashCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s for trailing slash, got %s", expectedTilde, tildeSlashCfg.GetWorkspaceDir())
	}

	// 4. Environment variable expansion ($HOME)
	envCfg := Config{Server: &ServerConfig{WorkspaceDir: "$HOME/my-project"}}
	if envCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s for $HOME expansion, got %s", expectedTilde, envCfg.GetWorkspaceDir())
	}

	envBraceCfg := Config{Server: &ServerConfig{WorkspaceDir: "${HOME}/my-project/"}}
	if envBraceCfg.GetWorkspaceDir() != expectedTilde {
		t.Errorf("expected %s for ${HOME} with trailing slash, got %s", expectedTilde, envBraceCfg.GetWorkspaceDir())
	}

	// 5. JSON roundtrip
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	if loaded.Server == nil || loaded.Server.WorkspaceDir != tmpDir {
		t.Errorf("expected WorkspaceDir %s, got %v", tmpDir, loaded.Server)
	}
}

func TestToolDefaults_WorkspaceScoping(t *testing.T) {
	wsDir := t.TempDir()
	absWs, _ := filepath.Abs(wsDir)

	g := NewGraph()
	storage := NewJSONLStorage(":memory:", "")
	mgr := NewManager(g, storage)
	mgr.RegisterDefaultTools(wsDir)

	tools := mgr.Registry.GetTools()
	toolsMap := make(map[string]Tool)
	for _, tool := range tools {
		toolsMap[tool.Name] = tool
	}

	ctx := context.Background()

	// 1. Test write_file in workspace
	writeTool, ok := toolsMap["write_file"]
	if !ok {
		t.Fatal("write_file tool not found")
	}
	_, err := writeTool.Function(ctx, map[string]interface{}{
		"path":    "hello.txt",
		"content": "Hello Workspace",
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	// Verify file was written inside wsDir
	writtenBytes, err := os.ReadFile(filepath.Join(absWs, "hello.txt"))
	if err != nil || string(writtenBytes) != "Hello Workspace" {
		t.Fatalf("file not found in workspace: %v, content: %s", err, string(writtenBytes))
	}

	// 2. Test read_file in workspace
	readTool, ok := toolsMap["read_file"]
	if !ok {
		t.Fatal("read_file tool not found")
	}
	content, err := readTool.Function(ctx, map[string]interface{}{
		"path": "hello.txt",
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if !strings.Contains(content, "Hello Workspace") {
		t.Errorf("expected content to contain 'Hello Workspace', got '%s'", content)
	}

	// 3. Test security sandbox: Path traversal rejected
	_, err = readTool.Function(ctx, map[string]interface{}{
		"path": "../../../etc/passwd",
	})
	if err == nil {
		t.Fatal("expected path traversal outside workspace to fail with security error")
	}
	if !strings.Contains(err.Error(), "outside of workspace root") && !strings.Contains(err.Error(), "outside of project root") {
		t.Errorf("expected security error, got: %v", err)
	}

	// 4. Test execute_command runs in workspace dir
	cmdTool, ok := toolsMap["execute_command"]
	if !ok {
		t.Fatal("execute_command tool not found")
	}
	pwdOut, err := cmdTool.Function(ctx, map[string]interface{}{
		"command": "pwd",
	})
	if err != nil {
		t.Fatalf("execute_command pwd failed: %v", err)
	}
	if strings.TrimSpace(pwdOut) != absWs {
		t.Errorf("expected execute_command to run in %s, got %s", absWs, strings.TrimSpace(pwdOut))
	}
}
