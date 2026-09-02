package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/engine"
)

// ChatStreamRequest defines the JSON payload sent by clients to initiate a generation turn.
type ChatStreamRequest struct {
	NodeID       string   `json:"node_id,omitempty"`
	ParentID     string   `json:"parent_id,omitempty"`
	Message      string   `json:"message"`
	Role         string   `json:"role,omitempty"` // default "user"
	Images       []string `json:"images,omitempty"`
	MaxToolDepth int      `json:"max_tool_depth,omitempty"` // default 10
}

// Event types for SSE protocol
const (
	EventThought      = "thought"
	EventToken        = "token"
	EventToolCall     = "tool_call"
	EventToolResult   = "tool_result"
	EventNodeComplete = "node_complete"
	EventError        = "error"
)

// SSE Payloads
type ThoughtPayload struct {
	Chunk string `json:"chunk"`
}

type TokenPayload struct {
	Chunk string `json:"chunk"`
}

type ToolCallPayload struct {
	ID        string                 `json:"id"`
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ToolResultPayload struct {
	ID     string `json:"id"`
	Tool   string `json:"tool"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

type NodeCompletePayload struct {
	NodeID    string `json:"node_id"`
	ParentID  string `json:"parent_id"`
	Role      string `json:"role"`
	Timestamp string `json:"timestamp"`
}

type ErrorPayload struct {
	Error string `json:"error"`
}

func sendSSE(w io.Writer, flusher http.Flusher, event string, data interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(bytes)); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

// handleChatStream processes POST /api/v1/chat/stream requests
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Read and decode request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req ChatStreamRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Message) == "" && len(req.Images) == 0 {
		http.Error(w, "Message or images are required", http.StatusBadRequest)
		return
	}

	if s.Provider == nil {
		http.Error(w, "LLM provider is not configured on server", http.StatusInternalServerError)
		return
	}

	// Set SSE Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// 1. Resolve existing user node if specified (e.g. created by client beforehand)
	var userNode *engine.Node
	if req.NodeID != "" {
		if existing, err := s.Manager.GetNode(req.NodeID); err == nil && existing != nil {
			userNode = existing
		} else {
			// Sync graph in case node was just written by client via POST /api/v1/nodes
			if _, _, syncErr := s.Manager.Sync(); syncErr == nil {
				if syncedNode, err := s.Manager.GetNode(req.NodeID); err == nil && syncedNode != nil {
					userNode = syncedNode
				}
			}
		}
	}

	// 2. Resolve parent ID if a new user node must be created
	parentID := req.ParentID
	if userNode == nil && parentID == "" {
		// Sync graph to get latest active leaf
		_, lastID, err := s.Manager.Sync()
		if err == nil && lastID != "" {
			parentID = lastID
		}
	}

	// 3. Create User Node if not already existing
	if userNode == nil {
		role := engine.RoleUser
		if req.Role != "" {
			role = engine.Role(req.Role)
		}

		var err error
		userNode, err = s.Manager.CreateNode(parentID, role, req.Message, false)
		if err != nil {
			_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: "Failed to create node: " + err.Error()})
			return
		}

		if len(req.Images) > 0 {
			s.Manager.AttachImages(userNode, req.Images)
			_ = s.Manager.Storage.SaveNode(userNode)
		}

		if s.EventBus != nil {
			s.EventBus.Publish(EventNodeSaved, map[string]interface{}{
				"node_id":   userNode.ID,
				"parent_id": userNode.ParentID,
				"role":      userNode.Role,
			})
		}
	}

	maxDepth := req.MaxToolDepth
	if maxDepth <= 0 && s.Config != nil && s.Config.Server != nil {
		maxDepth = s.Config.Server.GetMaxToolDepth()
	}
	if maxDepth <= 0 {
		maxDepth = 50
	}

	ctx := r.Context()
	currentParentID := userNode.ID

	// Multi-turn tool execution loop
	for depth := 0; depth < maxDepth; depth++ {
		supportsVision := false
		if s.Config != nil {
			supportsVision = s.Config.SupportsVision()
		}

		messages, err := s.Manager.BuildLLMContext(currentParentID, supportsVision)
		if err != nil {
			_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: "Context error: " + err.Error()})
			return
		}

		// Retrieve active tools from manager registry
		var tools []engine.Tool
		if s.Manager.Registry != nil {
			for _, t := range s.Manager.Registry.Tools {
				tools = append(tools, t)
			}
		}

		contentChan, thoughtChan, toolCallsChan, errChan := s.Provider.GenerateResponseStream(ctx, messages, tools)

		var fullContent strings.Builder
		var fullThought strings.Builder
		var accumulatedToolCalls []engine.ToolCall

		for contentChan != nil || thoughtChan != nil || toolCallsChan != nil || errChan != nil {
			select {
			case <-ctx.Done():
				return

			case thought, ok := <-thoughtChan:
				if !ok {
					thoughtChan = nil
				} else if thought != "" {
					fullThought.WriteString(thought)
					_ = sendSSE(w, flusher, EventThought, ThoughtPayload{Chunk: thought})
				}

			case chunk, ok := <-contentChan:
				if !ok {
					contentChan = nil
				} else if chunk != "" {
					fullContent.WriteString(chunk)
					_ = sendSSE(w, flusher, EventToken, TokenPayload{Chunk: chunk})
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
					_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: streamErr.Error()})
					return
				}
			}
		}

		// Save the Assistant Turn Node
		asstNode, err := s.Manager.CreateAssistantNode(
			currentParentID,
			fullContent.String(),
			fullThought.String(),
			accumulatedToolCalls,
			false,
		)
		if err != nil {
			_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: "Failed to persist assistant turn: " + err.Error()})
			return
		}

		currentParentID = asstNode.ID

		if s.EventBus != nil {
			s.EventBus.Publish(EventNodeSaved, map[string]interface{}{
				"node_id":   asstNode.ID,
				"parent_id": asstNode.ParentID,
				"role":      asstNode.Role,
			})
		}

		// If no tools were called, generation turn is complete!
		if len(accumulatedToolCalls) == 0 {
			_ = sendSSE(w, flusher, EventNodeComplete, NodeCompletePayload{
				NodeID:    asstNode.ID,
				ParentID:  asstNode.ParentID,
				Role:      string(asstNode.Role),
				Timestamp: asstNode.Timestamp.Format(time.RFC3339),
			})
			return
		}

		// Execute Tool Calls and stream tool results
		for _, call := range accumulatedToolCalls {
			var argsMap map[string]interface{}
			_ = json.Unmarshal(call.Function.Arguments, &argsMap)

			_ = sendSSE(w, flusher, EventToolCall, ToolCallPayload{
				ID:        call.ID,
				Tool:      call.Function.Name,
				Arguments: argsMap,
			})

			result, execErr := s.Manager.ExecuteToolCall(ctx, call)
			errStr := ""
			if execErr != nil {
				errStr = execErr.Error()
				result = fmt.Sprintf("Error: %s", execErr.Error())
			}

			// Update assistant observations or create tool node
			_ = s.Manager.UpdateAssistantObservations(asstNode.ID, call.ID, result)

			_ = sendSSE(w, flusher, EventToolResult, ToolResultPayload{
				ID:     call.ID,
				Tool:   call.Function.Name,
				Output: result,
				Error:  errStr,
			})
		}
	}

	// If maximum depth reached, notify completion with the latest assistant node
	_ = sendSSE(w, flusher, EventNodeComplete, NodeCompletePayload{
		NodeID:    currentParentID,
		ParentID:  userNode.ID,
		Role:      string(engine.RoleAssistant),
		Timestamp: time.Now().Format(time.RFC3339),
	})
}
