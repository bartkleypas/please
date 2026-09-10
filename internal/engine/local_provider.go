package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/bartkleypas/please/internal/tools"
)

// LocalHarnessProvider adapts SessionHarness into a Provider interface,
// allowing the standalone interactive TUI to consume the canonical multi-turn
// agent lifecycle in-process identically to a remote daemon client.
type LocalHarnessProvider struct {
	Harness   *SessionHarness
	SessionID string
	mu        sync.RWMutex
}

// NewLocalHarnessProvider instantiates a new LocalHarnessProvider.
func NewLocalHarnessProvider(harness *SessionHarness, sessionID string) *LocalHarnessProvider {
	if sessionID == "" {
		sessionID = "main"
	}
	return &LocalHarnessProvider{
		Harness:   harness,
		SessionID: sessionID,
	}
}

// SetSessionID updates the active session identifier.
func (p *LocalHarnessProvider) SetSessionID(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SessionID = sessionID
}

// GetSessionID returns the current session identifier.
func (p *LocalHarnessProvider) GetSessionID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.SessionID
}

// GenerateResponseStream executes the turn via SessionHarness and transforms
// harness events into content, thought, and error channel streams.
func (p *LocalHarnessProvider) GenerateResponseStream(ctx context.Context, messages []Message, availableTools []tools.Tool) (<-chan string, <-chan string, <-chan []ToolCall, <-chan error) {
	contentChan := make(chan string, 100)
	thoughtChan := make(chan string, 100)
	toolCallChan := make(chan []ToolCall) // Closed with zero items as harness executes tools internally
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
			if messages[i].Role == RoleUser {
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
			Role:       string(RoleUser),
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

// GenerateResponse executes the stream synchronously and returns the aggregated message.
func (p *LocalHarnessProvider) GenerateResponse(ctx context.Context, messages []Message, availableTools []tools.Tool) (*Message, error) {
	contentChan, _, _, errChan := p.GenerateResponseStream(ctx, messages, availableTools)
	var sb strings.Builder
	for chunk := range contentChan {
		sb.WriteString(chunk)
	}
	if err := <-errChan; err != nil {
		return nil, err
	}
	return &Message{
		Role:    RoleAssistant,
		Content: sb.String(),
	}, nil
}
