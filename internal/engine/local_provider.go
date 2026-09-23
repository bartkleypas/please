package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/worktree"
)

// LocalHarnessProvider adapts SessionHarness into a Provider interface,
// allowing the standalone interactive TUI to consume the canonical multi-turn
// agent lifecycle in-process identically to a remote daemon client.
type LocalHarnessProvider struct {
	Harness        *SessionHarness
	SessionID      string
	PrimaryManager *Manager
	mu             sync.RWMutex
}

// NewLocalHarnessProvider instantiates a new LocalHarnessProvider.
func NewLocalHarnessProvider(harness *SessionHarness, sessionID string) *LocalHarnessProvider {
	if sessionID == "" {
		sessionID = "main"
	}
	p := &LocalHarnessProvider{
		Harness:        harness,
		SessionID:      sessionID,
		PrimaryManager: harness.Manager,
	}
	p.updateHarnessWorkspace(sessionID)
	return p
}

// SetSessionID updates the active session identifier and re-binds worktree isolation if active.
func (p *LocalHarnessProvider) SetSessionID(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SessionID = sessionID
	p.updateHarnessWorkspace(sessionID)
}

func (p *LocalHarnessProvider) updateHarnessWorkspace(sessionID string) {
	if p.Harness == nil || p.Harness.Config == nil || !p.Harness.Config.EnableWorktreeIsolation() {
		return
	}
	if p.PrimaryManager == nil {
		p.PrimaryManager = p.Harness.Manager
	}

	if sessionID == "" || sessionID == "main" {
		p.Harness.Manager = p.PrimaryManager
		return
	}

	configDir, _ := config.GetConfigDir()
	wtMgr := worktree.NewManager(configDir, p.PrimaryManager.WorkspaceDir)
	if wtMgr.IsGitAvailable() && wtMgr.IsGitRepo() {
		if wtDir, _, err := wtMgr.EnsureWorktree(sessionID); err == nil && wtDir != "" {
			p.Harness.Manager = p.PrimaryManager.CloneWithWorkspace(wtDir, p.PrimaryManager.WorkspaceDir)
		}
	}
}

// GetSessionID returns the current session identifier.
func (p *LocalHarnessProvider) GetSessionID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.SessionID
}

// GenerateResponseStream executes the turn via SessionHarness and transforms
// harness events into content, thought, and error channel streams.
func (p *LocalHarnessProvider) GenerateResponseStream(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (<-chan string, <-chan string, <-chan []domain.ToolCall, <-chan error) {
	contentChan := make(chan string, 100)
	thoughtChan := make(chan string, 100)
	toolCallChan := make(chan []domain.ToolCall) // Closed with zero items as harness executes tools internally
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(thoughtChan)
		defer close(toolCallChan)
		defer close(errChan)

		var userNodeID string
		var lastUserMessage string
		var parentID string
		var images []string

		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == domain.RoleUser {
				userNodeID = messages[i].ID
				lastUserMessage = messages[i].Content
				images = messages[i].Images
				parentID = messages[i].ParentID
				break
			}
		}

		sessionID := p.GetSessionID()
		turnReq := TurnRequest{
			SessionID:  sessionID,
			UserNodeID: userNodeID,
			ParentID:   parentID,
			Message:    lastUserMessage,
			Role:       string(domain.RoleUser),
			Images:     images,
		}

		eventCh := make(chan HarnessEvent, 64)
		turnErrCh := make(chan error, 1)

		go func() {
			_, err := p.Harness.ExecuteTurn(ctx, turnReq, eventCh)
			if err != nil && ctx.Err() == nil {
				turnErrCh <- err
			}
			close(eventCh)
		}()

		for ev := range eventCh {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			switch ev.Kind {
			case HarnessEventToken:
				contentChan <- ev.Chunk

			case HarnessEventThought:
				thoughtChan <- ev.Chunk

			case HarnessEventToolCall:
				argsBytes, _ := json.Marshal(ev.ToolArgs)
				thoughtChan <- fmt.Sprintf("\n🛠️  Executing %s(%s)...\n", ev.ToolName, string(argsBytes))

			case HarnessEventToolResult:
				if ev.ToolError != "" {
					thoughtChan <- fmt.Sprintf("⚠️  Tool error: %s\n", ev.ToolError)
				} else {
					preview := ev.ToolResult
					if len(preview) > 160 {
						preview = preview[:160] + "... (truncated)"
					}
					thoughtChan <- fmt.Sprintf("✅ Result: %s\n\n", preview)
				}

			case HarnessEventError:
				if ev.Err != nil {
					errChan <- ev.Err
					return
				}
			}
		}

		select {
		case err := <-turnErrCh:
			if err != nil {
				errChan <- err
			}
		default:
		}
	}()

	return contentChan, thoughtChan, toolCallChan, errChan
}

// RawProvider returns the underlying stateless LLM client.
func (p *LocalHarnessProvider) RawProvider() providers.Provider {
	if p.Harness != nil {
		return p.Harness.Provider
	}
	return nil
}

// GenerateResponse delegates directly to the underlying stateless LLM client
// for one-shot completions (e.g. summaries), avoiding harness state mutation or node creation.
func (p *LocalHarnessProvider) GenerateResponse(ctx context.Context, messages []domain.Message, availableTools []domain.ToolSpec) (*domain.Message, error) {
	if p.Harness != nil && p.Harness.Provider != nil {
		return p.Harness.Provider.GenerateResponse(ctx, messages, availableTools)
	}
	return nil, fmt.Errorf("underlying LLM provider not available")
}

// SupportsStreaming returns true as LocalHarnessProvider supports streaming.
func (p *LocalHarnessProvider) SupportsStreaming() bool {
	return true
}
