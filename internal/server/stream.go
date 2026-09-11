package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/engine"
)

// ChatStreamRequest defines the JSON payload sent by clients to initiate a generation turn.
type ChatStreamRequest struct {
	NodeID       string            `json:"node_id,omitempty"`
	ParentID     string            `json:"parent_id,omitempty"`
	Message      string            `json:"message"`
	Role         string            `json:"role,omitempty"` // default "user"
	Images       []string          `json:"images,omitempty"`
	MaxToolDepth int               `json:"max_tool_depth,omitempty"` // default 10
	ActiveFile   string            `json:"active_file,omitempty"`
	CursorLine   int               `json:"cursor_line,omitempty"`
	Context      map[string]string `json:"context,omitempty"`
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

	// Ingest optional client context for ambient telemetry
	sessionID := r.Header.Get("X-Please-Session-ID")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session")
	}
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session_id")
	}
	if sessionID == "" && req.Context != nil {
		sessionID = req.Context["session_id"]
	}
	if sessionID == "" {
		sessionID = "main"
	}
	log.Printf("[server] Dispatched chat stream turn to session '%s'", sessionID)

	if s.Actors == nil {
		_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: "server actors registry not initialized"})
		return
	}

	actor := s.Actors.GetOrCreate(sessionID)
	eventCh := make(chan engine.HarnessEvent, 64)
	turnReq := engine.TurnRequest{
		SessionID:    sessionID,
		UserNodeID:   req.NodeID,
		ParentID:     req.ParentID,
		Message:      req.Message,
		Role:         req.Role,
		Images:       req.Images,
		MaxToolDepth: req.MaxToolDepth,
		ActiveFile:   req.ActiveFile,
		CursorLine:   req.CursorLine,
		Context:      req.Context,
	}

	resultChan, err := actor.Submit(r.Context(), turnReq, eventCh)
	if err != nil {
		_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: err.Error()})
		return
	}

	for ev := range eventCh {
		switch ev.Kind {
		case engine.HarnessEventToken:
			_ = sendSSE(w, flusher, EventToken, TokenPayload{Chunk: ev.Chunk})
		case engine.HarnessEventThought:
			_ = sendSSE(w, flusher, EventThought, ThoughtPayload{Chunk: ev.Chunk})
		case engine.HarnessEventToolCall:
			_ = sendSSE(w, flusher, EventToolCall, ToolCallPayload{
				ID:        ev.ToolCallID,
				Tool:      ev.ToolName,
				Arguments: ev.ToolArgs,
			})
		case engine.HarnessEventToolResult:
			_ = sendSSE(w, flusher, EventToolResult, ToolResultPayload{
				ID:     ev.ToolCallID,
				Tool:   ev.ToolName,
				Output: ev.ToolResult,
				Error:  ev.ToolError,
			})
		case engine.HarnessEventNodeComplete:
			if ev.Node != nil {
				_ = sendSSE(w, flusher, EventNodeComplete, NodeCompletePayload{
					NodeID:    ev.Node.ID,
					ParentID:  ev.Node.ParentID,
					Role:      string(ev.Node.Role),
					Timestamp: ev.Node.Timestamp.Format(time.RFC3339),
				})
			}
		case engine.HarnessEventError:
			if ev.Err != nil {
				_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: ev.Err.Error()})
			}
		}
	}

	// Wait for turn completion
	select {
	case <-r.Context().Done():
		return
	case res := <-resultChan:
		if res.err != nil && r.Context().Err() == nil {
			_ = sendSSE(w, flusher, EventError, ErrorPayload{Error: res.err.Error()})
		}
	}
}
