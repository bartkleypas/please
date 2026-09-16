package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// HarnessEventKind defines the types of events emitted during turn execution.
type HarnessEventKind string

const (
	HarnessEventToken        HarnessEventKind = "token"
	HarnessEventThought      HarnessEventKind = "thought"
	HarnessEventToolCall     HarnessEventKind = "tool_call"
	HarnessEventToolResult   HarnessEventKind = "tool_result"
	HarnessEventNodeComplete HarnessEventKind = "node_complete"
	HarnessEventError        HarnessEventKind = "error"
)

// HarnessEvent represents a single observable event during turn execution.
type HarnessEvent struct {
	Kind HarnessEventKind

	// Chunk for token / thought
	Chunk string

	// For tool_call
	ToolCallID string
	ToolName   string
	ToolArgs   map[string]interface{}

	// For tool_result
	ToolResult string
	ToolError  string

	// For node_complete
	Node *Node

	// For error
	Err error
}

// TurnRequest defines the inputs required to execute an agent turn.
type TurnRequest struct {
	SessionID    string
	UserNodeID   string
	ParentID     string
	Message      string
	Role         string
	Images       []string
	MaxToolDepth int
	ActiveFile   string
	CursorLine   int
	Context      map[string]string
}

// SessionHarness orchestrates the sequential multi-turn agent lifecycle:
// context assembly, LLM streaming, tool execution, observation recording,
// and DAG state persistence.
type SessionHarness struct {
	Manager     *Manager
	Provider    Provider
	Config      *Config
	OnNodeSaved func(node *Node)
}

// NewSessionHarness creates an initialized SessionHarness instance.
func NewSessionHarness(mgr *Manager, provider Provider, cfg *Config) *SessionHarness {
	return &SessionHarness{
		Manager:  mgr,
		Provider: provider,
		Config:   cfg,
	}
}

// ExecuteTurn executes a full multi-turn conversational cycle for a single session.
// It emits events to eventCh (if provided) and returns the final assistant Node upon completion.
func (h *SessionHarness) ExecuteTurn(ctx context.Context, req TurnRequest, eventCh chan<- HarnessEvent) (*Node, error) {
	emit := func(ev HarnessEvent) {
		if eventCh != nil {
			select {
			case eventCh <- ev:
			case <-ctx.Done():
			}
		}
	}

	// 0. Ambient client context setting
	if req.ActiveFile != "" || req.CursorLine > 0 || len(req.Context) > 0 {
		clientCtx := make(map[string]string)
		for k, v := range req.Context {
			clientCtx[k] = v
		}
		if req.ActiveFile != "" {
			clientCtx["active_file"] = req.ActiveFile
		}
		if req.CursorLine > 0 {
			clientCtx["cursor_line"] = fmt.Sprintf("%d", req.CursorLine)
		}
		if len(clientCtx) > 0 {
			h.Manager.SetClientContext(clientCtx)
			defer h.Manager.SetClientContext(nil)
		}
	}

	// 1. Resolve existing user node if specified (e.g. pre-created by client)
	var userNode *Node
	if req.UserNodeID != "" {
		if existing, err := h.Manager.GetNode(req.UserNodeID); err == nil && existing != nil {
			userNode = existing
		} else {
			if _, _, syncErr := h.Manager.Sync(); syncErr == nil {
				if syncedNode, err := h.Manager.GetNode(req.UserNodeID); err == nil && syncedNode != nil {
					userNode = syncedNode
				}
			}
		}
	}

	// 2. Resolve parent ID if a new user node must be created
	parentID := req.ParentID
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "main"
	}

	if userNode == nil && parentID == "" {
		if h.Manager != nil && h.Manager.Storage != nil {
			if headID, err := h.Manager.Storage.GetSessionHead(sessionID); err == nil && headID != "" {
				parentID = headID
			}
		}
		if parentID == "" {
			_, lastID, err := h.Manager.Sync()
			if err == nil && lastID != "" {
				parentID = lastID
			}
		}
	}

	// 3. Create User Node if not already existing
	if userNode == nil {
		role := RoleUser
		if req.Role != "" {
			role = Role(req.Role)
		}

		var err error
		userNode, err = h.Manager.CreateNode(parentID, role, req.Message, false)
		if err != nil {
			emit(HarnessEvent{Kind: HarnessEventError, Err: fmt.Errorf("failed to create node: %w", err)})
			return nil, err
		}

		if len(req.Images) > 0 {
			h.Manager.AttachImages(userNode, req.Images)
			_ = h.Manager.Storage.SaveNode(userNode)
		}

		if h.OnNodeSaved != nil {
			h.OnNodeSaved(userNode)
		}
	}

	maxDepth := req.MaxToolDepth
	if maxDepth <= 0 && h.Config != nil && h.Config.Server != nil {
		maxDepth = h.Config.Server.GetMaxToolDepth()
	}
	if maxDepth <= 0 {
		maxDepth = 15
	}

	var asstNode *Node
	var segments []AssistantSegment
	var lastToolKey string
	repeatCount := 0

	// Multi-turn tool execution loop
	for depth := 0; depth < maxDepth; depth++ {
		select {
		case <-ctx.Done():
			emit(HarnessEvent{Kind: HarnessEventError, Err: ctx.Err()})
			return asstNode, ctx.Err()
		default:
		}

		supportsVision := false
		if h.Config != nil {
			supportsVision = h.Config.SupportsVision()
		}

		contextNodeID := userNode.ID
		if asstNode != nil {
			if latest, err := h.Manager.GetNode(asstNode.ID); err == nil && latest != nil {
				asstNode = latest
			}
			contextNodeID = asstNode.ID
		}

		messages, err := h.Manager.BuildLLMContext(contextNodeID, supportsVision)
		if err != nil {
			emit(HarnessEvent{Kind: HarnessEventError, Err: fmt.Errorf("context error: %w", err)})
			return asstNode, err
		}

		var tools []Tool
		if h.Manager.Registry != nil {
			policy := ""
			if h.Config != nil {
				policy = h.Config.GetSandboxPolicy()
			}
			tools = h.Manager.Registry.GetToolsForPolicy(policy)
		}

		// Circuit Breaker 1: Runway Wrap-Up
		// On the final iteration of the runway (depth >= maxDepth - 1), suppress tools.
		// This forces the model to synthesize a final natural language response rather
		// than initiating a tool call that would be truncated abruptly without execution.
		if depth >= maxDepth-1 {
			tools = nil
		}

		// Circuit Breaker 2: Context Budget Exhaustion
		// If context fill ratio reaches or exceeds 85% capacity during multi-turn tool execution,
		// suppress tools to force a natural language summary before context overflow errors occur.
		if depth > 0 && h.Manager != nil {
			if fillRatio, _, err := h.Manager.EstimateContextFill(contextNodeID); err == nil && fillRatio >= 0.85 {
				tools = nil
			}
		}

		contentChan, thoughtChan, toolCallsChan, errChan := h.Provider.GenerateResponseStream(ctx, messages, tools)

		var fullContent strings.Builder
		var fullThought strings.Builder
		var accumulatedToolCalls []ToolCall

		for contentChan != nil || thoughtChan != nil || toolCallsChan != nil || errChan != nil {
			select {
			case <-ctx.Done():
				emit(HarnessEvent{Kind: HarnessEventError, Err: ctx.Err()})
				return asstNode, ctx.Err()

			case thought, ok := <-thoughtChan:
				if !ok {
					thoughtChan = nil
				} else if thought != "" {
					fullThought.WriteString(thought)
					emit(HarnessEvent{Kind: HarnessEventThought, Chunk: thought})
				}

			case chunk, ok := <-contentChan:
				if !ok {
					contentChan = nil
				} else if chunk != "" {
					fullContent.WriteString(chunk)
					emit(HarnessEvent{Kind: HarnessEventToken, Chunk: chunk})
				}

			case toolCalls, ok := <-toolCallsChan:
				if !ok {
					toolCallsChan = nil
				} else if len(toolCalls) > 0 {
					accumulatedToolCalls = append(accumulatedToolCalls, toolCalls...)
				}

			case streamErr, ok := <-errChan:
				if !ok {
					errChan = nil
				} else if streamErr != nil {
					emit(HarnessEvent{Kind: HarnessEventError, Err: streamErr})
					return asstNode, streamErr
				}
			}
		}

		contentChunk := fullContent.String()
		thoughtChunk := fullThought.String()

		// Fallback: check if model leaked raw tool tokens directly into content (e.g. Gemma <call>...</call>)
		if len(accumulatedToolCalls) == 0 {
			if cleanedContent, rawCalls := ExtractContentToolCalls(contentChunk); len(rawCalls) > 0 {
				contentChunk = cleanedContent
				accumulatedToolCalls = rawCalls
			}
		}

		cleanChunk, sigChunk := ExtractSignat(contentChunk)
		segContent := contentChunk
		if sigChunk != "" {
			segContent = cleanChunk
		}

		segments = append(segments, AssistantSegment{
			Content: segContent,
			Thought: thoughtChunk,
		})
		segBytes, _ := json.Marshal(segments)

		if asstNode == nil {
			var err error
			asstNode, err = h.Manager.CreateAssistantNode(
				userNode.ID,
				cleanChunk,
				thoughtChunk,
				accumulatedToolCalls,
				false,
			)
			if err != nil {
				emit(HarnessEvent{Kind: HarnessEventError, Err: fmt.Errorf("failed to persist assistant turn: %w", err)})
				return nil, err
			}
			if asstNode.Metadata == nil {
				asstNode.Metadata = make(map[string]string)
			}
			if sigChunk != "" {
				asstNode.Metadata["signat"] = sigChunk
			}
			asstNode.Metadata["segments"] = string(segBytes)
			_ = h.Manager.Storage.SaveNode(asstNode)

			if h.OnNodeSaved != nil {
				h.OnNodeSaved(asstNode)
			}
		} else {
			if latest, err := h.Manager.GetNode(asstNode.ID); err == nil && latest != nil {
				asstNode = latest
			}
			asstNode.Content += cleanChunk
			asstNode.Thought += thoughtChunk
			asstNode.ToolCalls = append(asstNode.ToolCalls, accumulatedToolCalls...)
			if asstNode.Metadata == nil {
				asstNode.Metadata = make(map[string]string)
			}
			if sigChunk != "" {
				asstNode.Metadata["signat"] = sigChunk
			}
			asstNode.Metadata["segments"] = string(segBytes)
			_ = h.Manager.Storage.SaveNode(asstNode)
		}

		// If no tools were called, generation turn is complete!
		if len(accumulatedToolCalls) == 0 {
			if latest, err := h.Manager.GetNode(asstNode.ID); err == nil && latest != nil {
				asstNode = latest
			}
			// Clean any trailing signat from the final assistant content into metadata
			clean, sig := ExtractSignat(asstNode.Content)
			if sig == "" && asstNode.Metadata != nil {
				sig = asstNode.Metadata["signat"]
			}
			asstNode.Content = clean
			if asstNode.Metadata == nil {
				asstNode.Metadata = make(map[string]string)
			}
			if sig != "" {
				asstNode.Metadata["signat"] = sig
			}
			if len(segments) > 0 {
				lastClean, _ := ExtractSignat(segments[len(segments)-1].Content)
				segments[len(segments)-1].Content = lastClean
				if segJSON, err := json.Marshal(segments); err == nil {
					asstNode.Metadata["segments"] = string(segJSON)
				}
			}
			_ = h.Manager.Storage.SaveNode(asstNode)

			if h.Manager != nil && h.Manager.Storage != nil {
				_ = h.Manager.Storage.SaveSessionHead(sessionID, asstNode.ID)
			}

			emit(HarnessEvent{
				Kind: HarnessEventNodeComplete,
				Node: asstNode,
			})
			return asstNode, nil
		}

		// Execute Tool Calls and stream tool results
		for _, call := range accumulatedToolCalls {
			select {
			case <-ctx.Done():
				emit(HarnessEvent{Kind: HarnessEventError, Err: ctx.Err()})
				return asstNode, ctx.Err()
			default:
			}

			var argsMap map[string]interface{}
			_ = json.Unmarshal(call.Function.Arguments, &argsMap)

			emit(HarnessEvent{
				Kind:       HarnessEventToolCall,
				ToolCallID: call.ID,
				ToolName:   call.Function.Name,
				ToolArgs:   argsMap,
			})

			callKey := fmt.Sprintf("%s:%s", call.Function.Name, string(call.Function.Arguments))
			if callKey == lastToolKey {
				repeatCount++
			} else {
				lastToolKey = callKey
				repeatCount = 1
			}

			var result string
			var execErr error
			var errStr string

			if repeatCount >= 3 {
				execErr = fmt.Errorf("loop circuit breaker triggered: tool %q invoked with identical arguments 3 times consecutively", call.Function.Name)
				errStr = execErr.Error()
				result = fmt.Sprintf("Error: %s. Please synthesize your final response or adjust parameters.", execErr.Error())
			} else {
				result, execErr = h.Manager.ExecuteToolCall(ctx, call)
				if execErr != nil {
					errStr = execErr.Error()
					result = fmt.Sprintf("Error: %s", execErr.Error())
				}
			}

			// Update assistant observations on the unified assistant node
			_ = h.Manager.UpdateAssistantObservations(asstNode.ID, call.ID, result)
			if latest, err := h.Manager.GetNode(asstNode.ID); err == nil && latest != nil {
				asstNode = latest
			}

			emit(HarnessEvent{
				Kind:       HarnessEventToolResult,
				ToolCallID: call.ID,
				ToolName:   call.Function.Name,
				ToolResult: result,
				ToolError:  errStr,
			})
		}
	}

	// If maximum depth reached, finalize node and notify completion
	if asstNode != nil {
		if h.Manager != nil && h.Manager.Storage != nil {
			_ = h.Manager.Storage.SaveSessionHead(sessionID, asstNode.ID)
		}
		emit(HarnessEvent{
			Kind: HarnessEventNodeComplete,
			Node: asstNode,
		})
	}

	return asstNode, nil
}
