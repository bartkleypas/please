package main

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

type e2eClient struct {
	mu       sync.Mutex
	messages []string
}

func (c *e2eClient) ReadTextFile(ctx context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, nil
}

func (c *e2eClient) WriteTextFile(ctx context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, nil
}

func (c *e2eClient) RequestPermission(ctx context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.NewRequestPermissionOutcomeSelected("allow_once"),
	}, nil
}

func (c *e2eClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if params.Update.AgentMessageChunk != nil && params.Update.AgentMessageChunk.Content.Text != nil {
		c.messages = append(c.messages, params.Update.AgentMessageChunk.Content.Text.Text)
	}
	return nil
}

func (c *e2eClient) CreateTerminal(ctx context.Context, params acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, nil
}

func (c *e2eClient) KillTerminal(ctx context.Context, params acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, nil
}

func (c *e2eClient) TerminalOutput(ctx context.Context, params acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, nil
}

func (c *e2eClient) ReleaseTerminal(ctx context.Context, params acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func (c *e2eClient) WaitForTerminalExit(ctx context.Context, params acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, nil
}

func TestACP_BinaryE2E(t *testing.T) {
	if os.Getenv("PLEASE_E2E") == "" && os.Getenv("PLEASE_LIVE_FIRE") == "" {
		t.Skip("Skipping binary E2E test (set PLEASE_E2E=1 to run)")
	}

	if _, err := os.Stat("../../please"); os.IsNotExist(err) {
		t.Skip("Skipping binary E2E test; please binary not built (run make build)")
	}

	configPath := "../../e2e.json"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = "../../livefire.json"
	}
	vaultPath := "../../test_vault/e2e.db"
	if _, err := os.Stat(vaultPath); os.IsNotExist(err) {
		vaultPath = "../../test_vault/livefire.db"
	}

	// Test multicall invocation via please-acp (simulates Xcode calling the symlink directly with no subcommand argument)
	cmd := exec.Command("../../please-acp", "-c", configPath, "-v", vaultPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("failed to open stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to open stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	client := &e2eClient{}
	conn := acpsdk.NewClientSideConnection(client, stdin, stdout)

	handshakeTimeout := 1 * time.Minute
	promptTimeout := 3 * time.Minute
	if custom := os.Getenv("PLEASE_E2E_TIMEOUT"); custom != "" {
		if d, err := time.ParseDuration(custom); err == nil {
			promptTimeout = d
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancel()

	// 1. Handshake (Initialize)
	initResp, err := conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: 1,
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Logf("Initialize SUCCESS: Agent %s %s (protocol v%d)", initResp.AgentInfo.Name, initResp.AgentInfo.Version, initResp.ProtocolVersion)

	// 2. NewSession
	sessionResp, err := conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        "../../",
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	t.Logf("NewSession SUCCESS: SessionId = %s", sessionResp.SessionId)

	// 3. ListSessions
	listResp, err := conn.ListSessions(ctx, acpsdk.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	t.Logf("ListSessions SUCCESS: Found %d sessions", len(listResp.Sessions))

	// 4. Prompt (Real LLM inference through stdio process)
	t.Log("Sending Prompt to agent over stdio...")
	promptCtx, promptCancel := context.WithTimeout(context.Background(), promptTimeout)
	defer promptCancel()

	promptResp, err := conn.Prompt(promptCtx, acpsdk.PromptRequest{
		SessionId: sessionResp.SessionId,
		Prompt: []acpsdk.ContentBlock{
			acpsdk.TextBlock("Respond with only: ACP online"),
		},
	})

	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	t.Logf("Prompt SUCCESS: StopReason = %s, Messages received = %v", promptResp.StopReason, client.messages)
}
