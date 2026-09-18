package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/tools"
	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
)

// Agent implements the acpsdk.Agent interface, adapting the please SessionHarness,
// DAG storage, and tool registry to the Agent Client Protocol.
type Agent struct {
	mgr          *engine.Manager
	provider     engine.Provider
	cfg          *engine.Config
	workspaceDir string

	connMu sync.RWMutex
	conn   *acpsdk.AgentSideConnection

	// Per-session state
	sessionCancels sync.Map // map[acpsdk.SessionId]context.CancelFunc
	sessionModes   sync.Map // map[acpsdk.SessionId]string
	sessionCwd     sync.Map // map[acpsdk.SessionId]string
	allowedTools   sync.Map // map[acpsdk.SessionId]map[string]bool
	allowedMu      sync.Mutex
}

// NewAgent constructs an initialized Agent adapter.
func NewAgent(mgr *engine.Manager, provider engine.Provider, cfg *engine.Config, workspaceDir string) *Agent {
	if workspaceDir == "" && cfg != nil {
		workspaceDir = cfg.GetWorkspaceDir()
	}
	return &Agent{
		mgr:          mgr,
		provider:     provider,
		cfg:          cfg,
		workspaceDir: workspaceDir,
	}
}

// SetConnection stores the active AgentSideConnection for sending updates and permission requests.
func (a *Agent) SetConnection(conn *acpsdk.AgentSideConnection) {
	a.connMu.Lock()
	defer a.connMu.Unlock()
	a.conn = conn
}

func (a *Agent) getConnection() *acpsdk.AgentSideConnection {
	a.connMu.RLock()
	defer a.connMu.RUnlock()
	return a.conn
}

func (a *Agent) isToolAllowedAlways(sessionID, toolName string) bool {
	sid := acpsdk.SessionId(sessionID)
	if val, ok := a.allowedTools.Load(sid); ok {
		if m, ok := val.(map[string]bool); ok {
			return m[toolName]
		}
	}
	return false
}

func (a *Agent) setToolAllowedAlways(sessionID, toolName string) {
	a.allowedMu.Lock()
	defer a.allowedMu.Unlock()
	sid := acpsdk.SessionId(sessionID)
	var m map[string]bool
	if val, ok := a.allowedTools.Load(sid); ok {
		if existing, ok := val.(map[string]bool); ok {
			m = existing
		}
	}
	if m == nil {
		m = make(map[string]bool)
	}
	m[toolName] = true
	a.allowedTools.Store(sid, m)
}

func (a *Agent) availableModes(current string) *acpsdk.SessionModeState {
	strictDesc := "Read-only inspection; zero workspace mutations or shell execution permitted."
	standardDesc := "Safe workspace mutations permitted (file edits/writes); raw shell commands blocked."
	permissiveDesc := "Host command execution enabled with interactive consent gates for operator approval."

	modes := []acpsdk.SessionMode{
		{
			Id:          acpsdk.SessionModeId(tools.SandboxPolicyStrict),
			Name:        "Strict (Read-Only)",
			Description: &strictDesc,
		},
		{
			Id:          acpsdk.SessionModeId(tools.SandboxPolicyStandard),
			Name:        "Standard (Safe Workspace Edits)",
			Description: &standardDesc,
		},
		{
			Id:          acpsdk.SessionModeId(tools.SandboxPolicyPermissive),
			Name:        "Permissive (Gated Shell Execution)",
			Description: &permissiveDesc,
		},
	}

	cur := acpsdk.SessionModeId(current)
	if cur == "" {
		cur = acpsdk.SessionModeId(tools.SandboxPolicyStandard)
	}

	return &acpsdk.SessionModeState{
		AvailableModes: modes,
		CurrentModeId:  cur,
	}
}

// Initialize negotiates capabilities and declares agent metadata.
func (a *Agent) Initialize(ctx context.Context, params acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	name := "please"
	version := engine.Version
	if version == "" {
		version = "dev"
	}
	return acpsdk.InitializeResponse{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		AgentInfo: &acpsdk.Implementation{
			Name:    name,
			Version: version,
		},
		AgentCapabilities: acpsdk.AgentCapabilities{
			SessionCapabilities: acpsdk.SessionCapabilities{
				Close:  &acpsdk.SessionCloseCapabilities{},
				List:   &acpsdk.SessionListCapabilities{},
				Resume: &acpsdk.SessionResumeCapabilities{},
			},
		},
	}, nil
}

// Authenticate handles authentication requests from the client.
func (a *Agent) Authenticate(ctx context.Context, params acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	return acpsdk.AuthenticateResponse{}, nil
}

// Logout terminates authenticated state.
func (a *Agent) Logout(ctx context.Context, params acpsdk.LogoutRequest) (acpsdk.LogoutResponse, error) {
	return acpsdk.LogoutResponse{}, nil
}

// NewSession creates a new conversational session and advertises security modes.
func (a *Agent) NewSession(ctx context.Context, params acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	sessID := acpsdk.SessionId(fmt.Sprintf("session-%s", uuid.New().String()[:8]))
	cwd := params.Cwd
	if cwd == "" {
		cwd = a.workspaceDir
	}
	a.sessionCwd.Store(sessID, cwd)

	defaultMode := tools.SandboxPolicyStandard
	if a.cfg != nil {
		if pol := a.cfg.GetSandboxPolicy(); pol != "" {
			defaultMode = pol
		}
	}
	a.sessionModes.Store(sessID, defaultMode)

	return acpsdk.NewSessionResponse{
		SessionId: sessID,
		Modes:     a.availableModes(defaultMode),
	}, nil
}

// ListSessions returns historical sessions from storage.
func (a *Agent) ListSessions(ctx context.Context, params acpsdk.ListSessionsRequest) (acpsdk.ListSessionsResponse, error) {
	if a.mgr == nil || a.mgr.Storage == nil {
		return acpsdk.ListSessionsResponse{}, nil
	}

	sessionHeads, err := a.mgr.Storage.ListSessions()
	if err != nil {
		return acpsdk.ListSessionsResponse{}, err
	}

	sessions := make([]acpsdk.SessionInfo, 0, len(sessionHeads))
	for id, headID := range sessionHeads {
		cwd := a.workspaceDir
		if val, ok := a.sessionCwd.Load(acpsdk.SessionId(id)); ok {
			if s, ok := val.(string); ok && s != "" {
				cwd = s
			}
		}

		info := acpsdk.SessionInfo{
			SessionId: acpsdk.SessionId(id),
			Cwd:       cwd,
		}

		node, err := a.mgr.GetNode(headID)
		if (err != nil || node == nil) && a.mgr != nil {
			_, _, _ = a.mgr.Sync()
			node, _ = a.mgr.GetNode(headID)
		}

		if node != nil {
			title := node.Content
			if len(title) > 60 {
				title = title[:57] + "..."
			}
			if title != "" {
				info.Title = &title
			}
			ts := node.Timestamp.UTC().Format(time.RFC3339)
			info.UpdatedAt = &ts
		}

		sessions = append(sessions, info)
	}

	return acpsdk.ListSessionsResponse{
		Sessions: sessions,
	}, nil
}

// ResumeSession reconnects to an existing session and restores its security mode.
func (a *Agent) ResumeSession(ctx context.Context, params acpsdk.ResumeSessionRequest) (acpsdk.ResumeSessionResponse, error) {
	if params.Cwd != "" {
		a.sessionCwd.Store(params.SessionId, params.Cwd)
	}

	currentMode := tools.SandboxPolicyStandard
	if val, ok := a.sessionModes.Load(params.SessionId); ok {
		if s, ok := val.(string); ok && s != "" {
			currentMode = s
		}
	} else if a.cfg != nil {
		if pol := a.cfg.GetSandboxPolicy(); pol != "" {
			currentMode = pol
		}
	}

	return acpsdk.ResumeSessionResponse{
		Modes: a.availableModes(currentMode),
	}, nil
}

// SetSessionMode updates the active sandbox security policy for a session.
func (a *Agent) SetSessionMode(ctx context.Context, params acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	mode := strings.ToLower(string(params.ModeId))
	switch mode {
	case tools.SandboxPolicyStrict, tools.SandboxPolicyStandard, tools.SandboxPolicyPermissive:
		a.sessionModes.Store(params.SessionId, mode)
		if conn := a.getConnection(); conn != nil {
			_ = conn.SessionUpdate(ctx, acpsdk.SessionNotification{
				SessionId: params.SessionId,
				Update: acpsdk.SessionUpdate{
					CurrentModeUpdate: &acpsdk.SessionCurrentModeUpdate{
						CurrentModeId: params.ModeId,
					},
				},
			})
		}
		return acpsdk.SetSessionModeResponse{}, nil
	default:
		return acpsdk.SetSessionModeResponse{}, fmt.Errorf("unknown mode: %s", params.ModeId)
	}
}

// SetSessionConfigOption is a stub for configuration updates.
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	return acpsdk.SetSessionConfigOptionResponse{}, nil
}

// Cancel cancels any in-flight prompt turn for the session.
func (a *Agent) Cancel(ctx context.Context, params acpsdk.CancelNotification) error {
	if val, ok := a.sessionCancels.Load(params.SessionId); ok {
		if cancel, ok := val.(context.CancelFunc); ok {
			cancel()
		}
	}
	return nil
}

// CloseSession cancels active operations and releases in-memory session state.
func (a *Agent) CloseSession(ctx context.Context, params acpsdk.CloseSessionRequest) (acpsdk.CloseSessionResponse, error) {
	_ = a.Cancel(ctx, acpsdk.CancelNotification{SessionId: params.SessionId})
	a.sessionCancels.Delete(params.SessionId)
	a.sessionModes.Delete(params.SessionId)
	a.sessionCwd.Delete(params.SessionId)
	a.allowedTools.Delete(params.SessionId)
	return acpsdk.CloseSessionResponse{}, nil
}

// Prompt processes a user prompt turn, streaming events and bridging the permission gate.
func (a *Agent) Prompt(ctx context.Context, params acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	var sb strings.Builder
	var images []string
	for _, block := range params.Prompt {
		if block.Text != nil {
			sb.WriteString(block.Text.Text)
		}
		if block.ResourceLink != nil {
			sb.WriteString(fmt.Sprintf("\n[Reference: %s (%s)]\n", block.ResourceLink.Name, block.ResourceLink.Uri))
		}
		if block.Resource != nil && block.Resource.Resource.TextResourceContents != nil {
			sb.WriteString(fmt.Sprintf("\n--- Content from %s ---\n%s\n---\n", block.Resource.Resource.TextResourceContents.Uri, block.Resource.Resource.TextResourceContents.Text))
		}
		if block.Image != nil && block.Image.Data != "" {
			images = append(images, block.Image.Data)
		}
	}

	turnCtx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()
	a.sessionCancels.Store(params.SessionId, turnCancel)
	defer a.sessionCancels.Delete(params.SessionId)

	// Determine active sandbox policy for this session
	mode := tools.SandboxPolicyStandard
	if val, ok := a.sessionModes.Load(params.SessionId); ok {
		if s, ok := val.(string); ok && s != "" {
			mode = s
		}
	} else if a.cfg != nil {
		if pol := a.cfg.GetSandboxPolicy(); pol != "" {
			mode = pol
		}
	}

	// Clone config with session policy override
	turnCfg := engine.NewDefaultConfig()
	if a.cfg != nil {
		*turnCfg = *a.cfg
		if a.cfg.Server != nil {
			serverCopy := *a.cfg.Server
			turnCfg.Server = &serverCopy
		}
	}
	if turnCfg.Server != nil {
		turnCfg.Server.SandboxPolicy = mode
	}

	// Bind session working directory if provided by client
	if val, ok := a.sessionCwd.Load(params.SessionId); ok {
		if sCwd, ok := val.(string); ok && sCwd != "" {
			if turnCfg.Server != nil {
				turnCfg.Server.WorkspaceDir = sCwd
			}
		}
	}

	harness := engine.NewSessionHarness(a.mgr, a.provider, turnCfg)

	// Configure interactive permission gate

	harness.PermissionGate = func(pCtx context.Context, sessionID string, call engine.ToolCall) (bool, error) {
		if a.isToolAllowedAlways(sessionID, call.Function.Name) {
			return true, nil
		}

		conn := a.getConnection()
		if conn == nil {
			return true, nil
		}

		title := fmt.Sprintf("Execute %s", call.Function.Name)
		pendingStatus := acpsdk.ToolCallStatusPending
		optAllowOnce := acpsdk.PermissionOption{
			OptionId: "allow_once",
			Kind:     acpsdk.PermissionOptionKindAllowOnce,
			Name:     "Approve",
		}
		optAllowAlways := acpsdk.PermissionOption{
			OptionId: "allow_always",
			Kind:     acpsdk.PermissionOptionKindAllowAlways,
			Name:     "Always allow for this session",
		}
		optRejectOnce := acpsdk.PermissionOption{
			OptionId: "reject_once",
			Kind:     acpsdk.PermissionOptionKindRejectOnce,
			Name:     "Skip / Deny",
		}

		req := acpsdk.RequestPermissionRequest{
			SessionId: params.SessionId,
			ToolCall: acpsdk.ToolCallUpdate{
				ToolCallId: acpsdk.ToolCallId(call.ID),
				Title:      &title,
				Status:     &pendingStatus,
				RawInput:   json.RawMessage(call.Function.Arguments),
			},
			Options: []acpsdk.PermissionOption{optAllowOnce, optAllowAlways, optRejectOnce},
		}

		resp, err := conn.RequestPermission(pCtx, req)
		if err != nil {
			return false, err
		}

		if resp.Outcome.Cancelled != nil {
			return false, context.Canceled
		}

		if resp.Outcome.Selected != nil {
			switch resp.Outcome.Selected.OptionId {
			case "allow_always":
				a.setToolAllowedAlways(sessionID, call.Function.Name)
				return true, nil
			case "allow_once":
				return true, nil
			case "reject_once":
				return false, nil
			default:
				return false, nil
			}
		}

		return false, nil
	}

	cleanedPrompt, activeFile, cursorLine := ParseClientPrompt(sb.String())

	eventCh := make(chan engine.HarnessEvent, 64)
	turnReq := engine.TurnRequest{
		SessionID:  string(params.SessionId),
		Message:    cleanedPrompt,
		Images:     images,
		ActiveFile: activeFile,
		CursorLine: cursorLine,
	}


	errTurnCh := make(chan error, 1)
	go func() {
		_, err := harness.ExecuteTurn(turnCtx, turnReq, eventCh)
		close(eventCh)
		errTurnCh <- err
	}()

	conn := a.getConnection()
	for ev := range eventCh {
		if conn == nil {
			continue
		}
		switch ev.Kind {
		case engine.HarnessEventToken:
			_ = conn.SessionUpdate(turnCtx, acpsdk.SessionNotification{
				SessionId: params.SessionId,
				Update:    acpsdk.UpdateAgentMessageText(ev.Chunk),
			})
		case engine.HarnessEventThought:
			_ = conn.SessionUpdate(turnCtx, acpsdk.SessionNotification{
				SessionId: params.SessionId,
				Update:    acpsdk.UpdateAgentThoughtText(ev.Chunk),
			})
		case engine.HarnessEventToolCall:
			_ = conn.SessionUpdate(turnCtx, acpsdk.SessionNotification{
				SessionId: params.SessionId,
				Update: acpsdk.StartToolCall(
					acpsdk.ToolCallId(ev.ToolCallID),
					ev.ToolName,
					acpsdk.WithStartStatus(acpsdk.ToolCallStatusInProgress),
					acpsdk.WithStartRawInput(ev.ToolArgs),
				),
			})
		case engine.HarnessEventToolResult:
			status := acpsdk.ToolCallStatusCompleted
			if ev.ToolError != "" {
				status = acpsdk.ToolCallStatusFailed
			}
			_ = conn.SessionUpdate(turnCtx, acpsdk.SessionNotification{
				SessionId: params.SessionId,
				Update: acpsdk.UpdateToolCall(
					acpsdk.ToolCallId(ev.ToolCallID),
					acpsdk.WithUpdateStatus(status),
					acpsdk.WithUpdateRawOutput(ev.ToolResult),
				),
			})
		}
	}

	turnErr := <-errTurnCh
	if turnErr != nil && turnErr != context.Canceled {
		return acpsdk.PromptResponse{}, turnErr
	}

	resp := acpsdk.PromptResponse{
		StopReason:    acpsdk.StopReasonEndTurn,
		UserMessageId: params.MessageId,
	}
	return resp, nil
}
