package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func setupLiveFire(t *testing.T) (*Manager, *OllamaProvider) {
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

	storage, err := NewSQLiteStorage(dbPath)
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

	// 2. Try loading a workspace-specific test config
	workspaceConfigPath := "livefire.json"
	if data, err := os.ReadFile(workspaceConfigPath); err == nil {
		var wsConfig struct {
			Endpoint string `json:"endpoint"`
			Model    string `json:"model"`
		}
		if json.Unmarshal(data, &wsConfig) == nil {
			if wsConfig.Endpoint != "" {
				endpoint = wsConfig.Endpoint
			}
			if wsConfig.Model != "" {
				model = wsConfig.Model
			}
		}
	} else {
		// 3. Fall back to the user's global please config
		if globalCfg, err := LoadConfig(); err == nil {
			if globalCfg.Endpoint != "" {
				endpoint = globalCfg.Endpoint
			}
			if globalCfg.Model != "" {
				model = globalCfg.Model
			}
		}
	}

	// 4. Environment variables can override everything
	if envModel := os.Getenv("OLLAMA_MODEL"); envModel != "" {
		model = envModel
	}
	if envEndpoint := os.Getenv("OLLAMA_ENDPOINT"); envEndpoint != "" {
		endpoint = envEndpoint
	}

	provider := NewOllamaProvider(endpoint, model)

	return mgr, provider
}

func mapPathToMessages(path []*Node) []Message {
	var messages []Message
	for _, n := range path {
		messages = append(messages, Message{
			Role:         n.Role,
			Content:      n.Content,
			Thought:      n.Thought,
			ToolCalls:    n.ToolCalls,
			ToolCallID:   n.ToolCallID,
			Observations: n.Observations,
			Internal:     n.Internal,
		})
	}
	return messages
}

func simulateTurn(t *testing.T, ctx context.Context, mgr *Manager, provider LLMProvider, input string, parentID string) *Node {
	t.Logf("### User says: %s", input)
	userNode, err := mgr.CreateNode(parentID, RoleUser, input, false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}

	path, err := mgr.GetPath(userNode.ID)
	if err != nil {
		t.Fatalf("failed to get path: %v", err)
	}

	messages := mapPathToMessages(path)

	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	t.Logf("### Assistant says: %s", resp.Content)
	assistantNode, err := mgr.CreateAssistantNode(userNode.ID, resp.Content, resp.Thought, resp.ToolCalls, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}

	return assistantNode
}

func executeToolTurn(t *testing.T, ctx context.Context, mgr *Manager, provider LLMProvider, tools []Tool, input string, parentID string) *Node {
	t.Logf("### User says: %s", input)
	userNode, err := mgr.CreateNode(parentID, RoleUser, input, false)
	if err != nil {
		t.Fatalf("failed to create user node: %v", err)
	}

	path, err := mgr.GetPath(userNode.ID)
	if err != nil {
		t.Fatalf("failed to get path: %v", err)
	}

	messages := mapPathToMessages(path)

	resp, err := provider.GenerateResponse(ctx, messages, tools)
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	toolCallsJSON, _ := json.Marshal(resp.ToolCalls)
	t.Logf("### Assistant emitted tool calls: %s", string(toolCallsJSON))
	if len(resp.ToolCalls) == 0 {
		t.Fatalf("expected tool calls, got none. Model responded with: %s", resp.Content)
	}

	assistantNode, err := mgr.CreateAssistantNode(userNode.ID, resp.Content, resp.Thought, resp.ToolCalls, false)
	if err != nil {
		t.Fatalf("failed to create assistant node: %v", err)
	}

	maxLoops := 5
	loopCount := 0
	currentResp := resp

	for len(currentResp.ToolCalls) > 0 && loopCount < maxLoops {
		loopCount++

		for _, call := range currentResp.ToolCalls {
			t.Logf("### Executing tool locally: %s", call.Function.Name)
			result, err := mgr.ExecuteToolCall(ctx, call)
			if err != nil {
				t.Logf("Tool execution returned error: %v", err)
				result = fmt.Sprintf("Error: %v", err)
			}

			t.Logf("### Tool Result: %s", result)

			callID := call.ID
			if callID == "" {
				callID = "call_" + call.Function.Name
			}

			err = mgr.UpdateAssistantObservations(assistantNode.ID, callID, result)
			if err != nil {
				t.Fatalf("failed to update observations: %v", err)
			}
		}

		// Refetch path to include new observations on the active node
		path, err = mgr.GetPath(assistantNode.ID)
		if err != nil {
			t.Fatalf("failed to get path: %v", err)
		}
		messages = mapPathToMessages(path)

		t.Logf("### Requesting follow-up response from Assistant (Loop %d)", loopCount)
		currentResp, err = provider.GenerateResponse(ctx, messages, tools)
		if err != nil {
			t.Fatalf("failed to generate follow-up response: %v", err)
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
