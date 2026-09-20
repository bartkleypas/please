package engine

import (
	"context"
	"encoding/json"
	"fmt"
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

	// 2. Try loading workspace-specific test config or global please config
	var activeCfg *Config
	if cfg, err := LoadConfigFile("livefire.json"); err == nil {
		activeCfg = cfg
	} else if cfg, err := LoadConfig(); err == nil {
		activeCfg = cfg
	}

	if activeCfg != nil && activeCfg.Server != nil {
		mgr.SignatSteering = activeCfg.EnableSignatSteering()
		mgr.AmbientTelemetry = activeCfg.EnableAmbientTelemetry()
		if activeCfg.Server.Provider != "" {
			providerType = activeCfg.Server.Provider
		}
		if activeCfg.Server.Endpoint != "" {
			endpoint = activeCfg.Server.Endpoint
		}
		if activeCfg.Server.Model != "" {
			model = activeCfg.Server.Model
		}
		if activeCfg.Server.APIKey != "" {
			apiKey = activeCfg.Server.APIKey
		}
		if activeCfg.Server.Options != nil {
			options = activeCfg.Server.Options
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

func finalizeAssistantTurn(mgr *Manager, asstNode *Node) *Node {
	if asstNode == nil {
		return nil
	}
	if latest, err := mgr.GetNode(asstNode.ID); err == nil && latest != nil {
		asstNode = latest
	}
	if clean, sig := ExtractSignat(asstNode.Content); sig != "" {
		asstNode.Content = clean
		if asstNode.Metadata == nil {
			asstNode.Metadata = make(map[string]string)
		}
		asstNode.Metadata["signat"] = sig
		var segments []AssistantSegment
		if segStr, ok := asstNode.Metadata["segments"]; ok && segStr != "" {
			_ = json.Unmarshal([]byte(segStr), &segments)
		}
		if len(segments) > 0 {
			if lastClean, lastSig := ExtractSignat(segments[len(segments)-1].Content); lastSig != "" {
				segments[len(segments)-1].Content = lastClean
			}
			if segJSON, err := json.Marshal(segments); err == nil {
				asstNode.Metadata["segments"] = string(segJSON)
			}
		}
		_ = mgr.Storage.SaveNode(asstNode)
	}
	return asstNode
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
	assistantNode = finalizeAssistantTurn(mgr, assistantNode)
	t.Logf("### [Node ID: %s] Assistant says: %s", assistantNode.ID, assistantNode.Content)

	return assistantNode
}

func simulateImageTurn(t *testing.T, ctx context.Context, mgr *Manager, provider LLMProvider, input string, imagePaths []string, parentID string) *Node {
	userNode, err := mgr.CreateNode(parentID, RoleUser, input, false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}
	mgr.AttachImages(userNode, imagePaths)
	t.Logf("### [Node ID: %s] User says: %s (Attached images: %v)", userNode.ID, input, imagePaths)

	supportsVision := false
	if op, ok := provider.(*OllamaProvider); ok {
		modelLower := strings.ToLower(op.Model)
		keywords := []string{"vision", "llava", "pixtral", "minicpm", "mplug", "bakllava", "llama3.2-vision", "llama-3.2-vision", "llama3-vision", "gemma4"}
		for _, kw := range keywords {
			if strings.Contains(modelLower, kw) {
				supportsVision = true
				break
			}
		}
	} else if _, ok := provider.(*OpenAIProvider); ok {
		supportsVision = true
	}
	messages, err := mgr.BuildLLMContext(userNode.ID, supportsVision)
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
	assistantNode = finalizeAssistantTurn(mgr, assistantNode)
	t.Logf("### [Node ID: %s] Assistant says: %s", assistantNode.ID, assistantNode.Content)

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

		// Update segments in metadata for causal history reconstruction (matching TUI streaming.go)
		var segments []AssistantSegment
		if assistantNode.Metadata == nil {
			assistantNode.Metadata = make(map[string]string)
		}
		if segStr, ok := assistantNode.Metadata["segments"]; ok && segStr != "" {
			_ = json.Unmarshal([]byte(segStr), &segments)
		}
		segments = append(segments, AssistantSegment{
			Content: currentResp.Content,
			Thought: currentResp.Thought,
		})
		if segJSON, err := json.Marshal(segments); err == nil {
			assistantNode.Metadata["segments"] = string(segJSON)
		}

		if err := mgr.Storage.SaveNode(assistantNode); err != nil {
			t.Fatalf("failed to update assistant node: %v", err)
		}
	}

	if loopCount == maxLoops {
		t.Logf("### Warning: Max tool loops reached")
	}

	assistantNode = finalizeAssistantTurn(mgr, assistantNode)
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
	input = "Please use the delete_file tool to remove 'test_vault/demo.txt'."
	turn8 := executeToolTurn(t, ctx, mgr, provider, tools, input, turn7.ID)

	// Feedback
	input = "We just tested write_file, list_directory, list_files_recursive, grep_search, edit_file, execute_command, and delete_file. Summarize your experience with these tools and their ease of use."
	summaryTurn := simulateTurn(t, ctx, mgr, provider, input, turn8.ID)

	// Image Reflection: Ask George to reflect on his portrait in Lore/George_image.png
	portraitPath := filepath.Join("Lore", "George_image.png")
	if _, err := os.Stat(portraitPath); err == nil {
		imagePrompt := "I found this portrait of you in Lore/George_image.png. Would you please take a look and describe the image to me?"
		simulateImageTurn(t, ctx, mgr, provider, imagePrompt, []string{portraitPath}, summaryTurn.ID)
	}
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

		// Update segments in metadata for causal history reconstruction (matching TUI streaming.go)
		var segments []AssistantSegment
		if assistantNode.Metadata == nil {
			assistantNode.Metadata = make(map[string]string)
		}
		if segStr, ok := assistantNode.Metadata["segments"]; ok && segStr != "" {
			_ = json.Unmarshal([]byte(segStr), &segments)
		}
		segments = append(segments, AssistantSegment{
			Content: currentResp.Content,
			Thought: currentResp.Thought,
		})
		if segJSON, err := json.Marshal(segments); err == nil {
			assistantNode.Metadata["segments"] = string(segJSON)
		}

		if err := mgr.Storage.SaveNode(assistantNode); err != nil {
			t.Fatalf("failed to update assistant node: %v", err)
		}
	}

	assistantNode = finalizeAssistantTurn(mgr, assistantNode)
	return assistantNode
}

func TestLLM_AutonomousNarrativeVector(t *testing.T) {
	mgr, provider := setupLiveFire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// 1. Establish George the Archivist Persona (Pure persona in Node 0 per ADR 003)
	sysPrompt := "You are George the Archivist 🦉📚, an 18-inch tall barred owl who chronicles software architecture. You speak with a sardonic wit and deep appreciation for structured DAGs."
	sysNode, err := mgr.CreateNode("", RoleSystem, sysPrompt, false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	var tools []Tool
	for _, tool := range mgr.Registry.Tools {
		tools = append(tools, tool)
	}

	// 2. Mission Primer (Signat steering flows purely from harness contract overlay)
	primerPrompt := "Greetings George! Over the next 5 turns, explore this workspace autonomously. On each turn, use the read_file tool to inspect a different core file (e.g. README.md, docs/context_resonance.md, GEMINI.md, internal/engine/service.go, internal/tui/map.go) and weave your findings into the lore of Please. Your continuation prompt will always be simply: 'Please proceed.'"
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
