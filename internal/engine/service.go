// package engine provides the core business logic for the Please application,
// including DAG management, LLM provider integration, and state persistence.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"path/filepath"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/bartkleypas/please/internal/tools"
	"github.com/google/uuid"
)

// Manager is the central coordinator for the application engine. It provides
// a high-level API that combines graph operations (traversal, branching)
// with storage persistence, ensuring that all narrative changes are saved.
// AssistantSegment represents a single turn segment of the assistant's generation,
// allowing sequential reconstruction of tool executions and narration.
type AssistantSegment struct {
	Content string `json:"content"`
	Thought string `json:"thought"`
}

// SignatSteeringContract defines the invariant output formatting instruction for turn signatures.
// Layered ephemerally onto the Genesis root node (RoleSystem) when signat_steering is enabled.
const SignatSteeringContract = "Conclude your response with a 1-3 emoji posture signature in `<signat></signat>` tags (e.g. <signat>🛠️💻</signat> for code/impl, <signat>🧠📐</signat> for logic/math, <signat>🔍📜</signat> for research/inspection, <signat>🎨✨</signat> for design/styling, <signat>📝💡</signat> for ideation, <signat>🦉☕</signat> for reflection)."

// const SignatSteeringContract = "To anchor your trajectory in the conversation map, conclude the text of each turn with a 1-3 emoji signature (signat) on the final line reflecting your active posture (e.g. 🛠️💻 for code/impl, 🧠📐 for logic/math, 🔍📜 for research/inspection, 🎨✨ for design/styling, 📝💡 for ideation, 🦉☕ for reflection)."

// AmbientTelemetryContract defines the attentional de-weighting instruction for peripheral environment data.
// Layered ephemerally onto the Genesis root node (RoleSystem) when ambient_telemetry is enabled.
const AmbientTelemetryContract = "You may receive peripheral environmental telemetry wrapped in <ADDITIONAL_METADATA> alongside user turns (such as current working directory, active git branch, or local timestamps). This metadata provides passive situational context. Do not recite, quote, or acknowledge this metadata in your responses unless the user explicitly asks about it."

// MemorySteeringContract defines the attentional de-weighting instruction for persistent memory invariants.
// Layered ephemerally onto the Genesis root node (RoleSystem) alongside recalled workspace constraints (ADR 014).
const MemorySteeringContract = "You have access to a persistent cybernetic memory vault.\nHigh-priority workspace constraints and architectural invariants are provided in <RECALLED_MEMORIES>.\nTreat these as established ground-truth invariants for this repository.\nDo not recite, quote, or acknowledge this block in your responses unless directly answering questions about them.\nWhen you discover a critical workspace invariant or fix a non-obvious bug, autonomously persist it using memory_store.\nDo not store conversational transcripts; the DAG already preserves turn history."

// Manager is the central coordinator for the application engine. It provides
// a high-level API that combines graph operations (traversal, branching)
// with storage persistence, ensuring that all narrative changes are saved.
type Manager struct {
	Graph            *graph.Graph
	Storage          storage.Storage
	Registry         *tools.ToolRegistry
	WorkspaceDir     string
	NumCtx           int
	SignatSteering   bool
	AmbientTelemetry bool
	clientContext    map[string]string
}

// NewManager creates a new Manager instance
func NewManager(g *graph.Graph, s storage.Storage) *Manager {
	return &Manager{
		Graph:        g,
		Storage:      s,
		Registry:     tools.NewToolRegistry(),
		WorkspaceDir: ".",
		NumCtx:       32768,
	}
}

// CloneWithWorkspace creates a lightweight copy of the manager scoped to a new workspace directory
// (such as a Git worktree), while sharing the same underlying Graph DAG, Storage vault, and
// runtime tuning parameters.
func (m *Manager) CloneWithWorkspace(workspaceDir string, primaryWorkspace ...string) *Manager {
	cloned := &Manager{
		Graph:            m.Graph,
		Storage:          m.Storage,
		Registry:         tools.NewToolRegistry(),
		WorkspaceDir:     workspaceDir,
		NumCtx:           m.NumCtx,
		SignatSteering:   m.SignatSteering,
		AmbientTelemetry: m.AmbientTelemetry,
		clientContext:    m.clientContext,
	}
	prim := m.WorkspaceDir
	if len(primaryWorkspace) > 0 && primaryWorkspace[0] != "" {
		prim = primaryWorkspace[0]
	}
	tools.RegisterDefaultTools(cloned.Registry, workspaceDir, prim)
	if memStore, ok := cloned.Storage.(storage.MemoryStore); ok && memStore != nil {
		cloned.Registry.RegisterMemory(NewMemoryToolsAdapter(memStore), "workspace")
	}
	return cloned
}

// SetClientContext sets temporary client editor context (e.g. active_file, cursor_line).
func (m *Manager) SetClientContext(ctx map[string]string) {
	m.clientContext = ctx
}

// GetClientContext returns the currently configured client editor context.
func (m *Manager) GetClientContext() map[string]string {
	return m.clientContext
}

// CreateNode handles the full lifecycle of creating a new node:
// ID generation, graph insertion, and persistence.
func (m *Manager) CreateNode(parentID string, role domain.Role, content string, internal bool) (*graph.Node, error) {
	id, _ := uuid.NewV7()
	cleanContent, signat := ExtractSignat(content)

	node := &graph.Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Internal:  internal,
		Metadata:  make(map[string]string),
	}

	if signat != "" && (role == domain.RoleSystem || role == domain.RoleSummary) {
		node.Metadata["signat"] = signat
		node.Content = cleanContent
	}

	if role == domain.RoleTool && node.ToolCallID == "" {
		node.ToolCallID = "cli_" + id.String()[:8]
	}

	if err := m.validateNode(node); err != nil {
		return nil, fmt.Errorf("node validation failed: %w", err)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist new node: %w", err)
	}

	return node, nil
}

// CreateAssistantNode creates a node for the assistant, potentially containing tool calls and reasoning
func (m *Manager) CreateAssistantNode(parentID string, content string, thought string, toolCalls []domain.ToolCall, internal bool) (*graph.Node, error) {
	id, _ := uuid.NewV7()
	cleanContent, signat := ExtractSignat(content)

	node := &graph.Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      domain.RoleAssistant,
		Content:   cleanContent,
		Thought:   thought,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
		Internal:  internal,
		Metadata:  make(map[string]string),
	}

	if signat != "" {
		node.Metadata["signat"] = signat
	} else {
		if silent := m.deriveSilentSignat(toolCalls, thought); silent != "" {
			node.Metadata["signat"] = silent
		}
	}

	segments := []AssistantSegment{
		{
			Content: cleanContent,
			Thought: thought,
		},
	}
	if segJSON, err := json.Marshal(segments); err == nil {
		node.Metadata["segments"] = string(segJSON)
	}

	if err := m.validateNode(node); err != nil {
		return nil, fmt.Errorf("assistant node validation failed: %w", err)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist assistant node: %w", err)
	}

	return node, nil
}

// deriveSilentSignat infers an ambient signat emoji from physical tool categories or internal thought.
// Used when signat_steering is disabled or the model omitted an explicit signat, ensuring
// the TUI /map and companion client graphs maintain 100% visual color coverage with zero prompt tax.
func (m *Manager) deriveSilentSignat(toolCalls []domain.ToolCall, thought string) string {
	if m.Registry != nil && len(toolCalls) > 0 {
		hasExecute := false
		hasMutate := false
		hasSensory := false
		for _, tc := range toolCalls {
			if tool, ok := m.Registry.Tools[tc.Function.Name]; ok {
				switch tool.Category {
				case domain.CategoryExecute:
					hasExecute = true
				case domain.CategoryMutate:
					hasMutate = true
				case domain.CategorySensory:
					hasSensory = true
				}
			}
		}
		if hasExecute {
			return "🧪⚡"
		}
		if hasMutate {
			return "🛠️💻"
		}
		if hasSensory {
			return "🔍📜"
		}
	}
	if len(thought) > 0 {
		return "🧠📐"
	}
	return "💬💭"
}

// CreateToolNode creates a node containing the result of a tool execution
func (m *Manager) CreateToolNode(parentID string, toolCallID string, content string, internal bool) (*graph.Node, error) {
	id, _ := uuid.NewV7()
	node := &graph.Node{
		ID:         id.String(),
		ParentID:   parentID,
		Role:       domain.RoleTool,
		Content:    content,
		Timestamp:  time.Now(),
		ToolCallID: toolCallID,
		Internal:   internal,
	}

	if err := m.validateNode(node); err != nil {
		return nil, fmt.Errorf("tool node validation failed: %w", err)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist tool node: %w", err)
	}

	return node, nil
}

// UpdateAssistantObservations appends side-channel tool results to an existing assistant node
func (m *Manager) UpdateAssistantObservations(nodeID string, callID string, result string) error {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		return err
	}

	if node.Role != domain.RoleAssistant {
		return fmt.Errorf("observations can only be added to assistant nodes")
	}

	node.Observations = append(node.Observations, domain.ToolObservation{
		ToolCallID: callID,
		Result:     result,
	})

	return m.Storage.UpdateNodeObservations(nodeID, node.Observations)
}

func (m *Manager) validateNode(node *graph.Node) error {
	if node.ID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}
	if node.ID == node.ParentID {
		return fmt.Errorf("node cannot be its own parent (cycle detected)")
	}
	if node.Role == domain.RoleUser && strings.TrimSpace(node.Content) == "" {
		return fmt.Errorf("user message content cannot be empty")
	}
	if node.Role == domain.RoleTool {
		if node.ToolCallID == "" {
			return fmt.Errorf("tool node must have a ToolCallID")
		}
		if strings.TrimSpace(node.Content) == "" {
			return fmt.Errorf("tool result content cannot be empty")
		}
	}
	return nil
}

// ExecuteToolCall runs the function associated with a tool call
func (m *Manager) ExecuteToolCall(ctx context.Context, call domain.ToolCall) (string, error) {
	if m.Registry == nil {
		return "", fmt.Errorf("tool registry is nil")
	}
	return m.Registry.Dispatch(ctx, call.Function.Name, call.Function.Arguments)
}

// Sync reloads the graph from storage, effectively synchronizing the in-memory state
// with any external changes (e.g., from other 'please' sessions).
func (m *Manager) Sync() (*graph.Graph, string, error) {
	g, lastID, err := m.Storage.LoadGraph()
	if err != nil {
		return nil, "", err
	}
	m.Graph = g
	return g, lastID, nil
}

// SetBookmark updates the bookmark status of a node in its metadata
func (m *Manager) SetBookmark(nodeID string, bookmarked bool) error {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		return err
	}

	if node.Metadata == nil {
		node.Metadata = make(map[string]string)
	}

	if bookmarked {
		node.Metadata["bookmarked"] = "true"
	} else {
		delete(node.Metadata, "bookmarked")
	}

	return nil
}

// calculateResonanceScore determines the context value of a node based on topological weight, compute cost, conversational distance, and temporal decay.
func (m *Manager) calculateResonanceScore(node *graph.Node, distance int, fillRatio float64, totalPathLen int) float64 {
	if node.Role == domain.RoleSystem || node.Role == domain.RoleSummary {
		return math.MaxFloat64
	}

	// Under 60% context capacity, keep 100% full fidelity across all public historical nodes
	if fillRatio < 0.60 && !node.Internal {
		return 100.0
	}

	weight := 0.7
	switch node.Role {
	case domain.RoleUser:
		weight = 1.0
	case domain.RoleTool:
		weight = 0.5
	}

	if node.Internal {
		weight = 0.05
	}
	if node.Metadata != nil && node.Metadata["bookmarked"] == "true" {
		weight = 2.0
	}

	cost := len(node.Content) + len(node.Thought)
	for _, obs := range node.Observations {
		cost += len(obs.Result)
	}
	if cost == 0 {
		cost = 1
	}

	baseScore := weight * 1000.0 / float64(cost)
	if baseScore > 20.0 {
		baseScore = 20.0
	}

	// Dynamic Grace Window based on capacity pressure
	graceTurns := 3
	kt := 0.02
	kd := 0.3

	if fillRatio < 0.85 {
		// Moderate load (60% - 85%): expand grace turns and slow decay rate
		graceTurns = int(float64(totalPathLen) * 0.5)
		if graceTurns < 5 {
			graceTurns = 5
		}
		kt = 0.01
		kd = 0.1
	}

	if distance < graceTurns {
		return baseScore
	}

	deltaMinutes := time.Since(node.Timestamp).Minutes()
	if deltaMinutes < 0 {
		deltaMinutes = 0
	}

	turnsPastGrace := float64(distance - graceTurns + 1)
	decayFactor := math.Exp(-kt*deltaMinutes) * math.Exp(-kd*turnsPastGrace)
	return baseScore * decayFactor
}

// CalculateResonanceScore computes the Context Resonance Score for a node given its distance and context metrics.
func (m *Manager) CalculateResonanceScore(node *graph.Node, distance int, fillRatio float64, totalPathLen int) float64 {
	return m.calculateResonanceScore(node, distance, fillRatio, totalPathLen)
}

// formatCompactedToolObservation produces a concise summary of a completed tool observation,
// preserving any leading pagination banner (e.g. "[Lines 1-64 of 131...]") so the model
// maintains memory of read ranges and continuation offsets without retaining the full body.
func formatCompactedToolObservation(toolName string, rawResult string) string {
	banner := ""
	if idx := strings.Index(rawResult, "\n"); idx != -1 {
		firstLine := strings.TrimSpace(rawResult[:idx])
		if strings.HasPrefix(firstLine, "[Lines ") || strings.HasPrefix(firstLine, "[Offset ") || (strings.HasPrefix(firstLine, "[") && strings.HasSuffix(firstLine, "]")) {
			banner = ": " + firstLine
		}
	} else if strings.HasPrefix(rawResult, "[Lines ") || strings.HasPrefix(rawResult, "[Offset ") || (strings.HasPrefix(rawResult, "[") && strings.HasSuffix(rawResult, "]")) {
		banner = ": " + strings.TrimSpace(rawResult)
	}

	return fmt.Sprintf("[Tool '%s' execution completed%s. Detailed results omitted. Total size: %d bytes.]", toolName, banner, len(rawResult))
}

// BuildLLMContext constructs the message history for the LLM, applying Priority Pruning based on the Context Resonance Score.
func (m *Manager) BuildLLMContext(leafID string, supportsVision bool) ([]domain.Message, error) {
	path, err := m.GetPath(leafID)
	if err != nil {
		return nil, err
	}

	// Calculate total path cost to determine fill ratio, accounting for ephemeral observation compaction
	var totalChars int
	for i, node := range path {
		distance := len(path) - 1 - i
		totalChars += len(node.Content) + len(node.Thought)
		totalObs := len(node.Observations)
		for j, obs := range node.Observations {
			obsLen := len(obs.Result)
			if distance >= 1 && obsLen > 1000 {
				obsLen = 150 // estimated compacted banner size
			} else if distance == 0 && totalObs > 2 && j < totalObs-2 && obsLen > 1000 {
				obsLen = 150
			}
			totalChars += obsLen
		}
	}

	limit := m.NumCtx
	if limit <= 0 {
		limit = 32768
	}
	estimatedTokens := int(float64(totalChars) / 3.8)
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}
	fillRatio := float64(estimatedTokens) / float64(limit)

	var messages []domain.Message
	for i, node := range path {
		distance := len(path) - 1 - i
		v := m.calculateResonanceScore(node, distance, fillRatio, len(path))

		// The active/latest node should always be kept in high fidelity regardless of score
		if distance == 0 {
			v = math.MaxFloat64
		}

		if node.Internal && v <= 0.5 {
			continue // Drop low fidelity internal nodes entirely
		}

		if node.Role == domain.RoleAssistant {
			var segments []AssistantSegment
			if node.Metadata != nil && node.Metadata["segments"] != "" {
				_ = json.Unmarshal([]byte(node.Metadata["segments"]), &segments)
			}

			obsMap := make(map[string]domain.ToolObservation, len(node.Observations))
			for _, obs := range node.Observations {
				obsMap[obs.ToolCallID] = obs
			}

			totalCalls := len(node.ToolCalls)
			formatObs := func(toolName, rawResult string, callIdx int) string {
				// Turn-boundary compaction: historical turns (distance >= 1) compact large observations
				if distance >= 1 && len(rawResult) > 1000 {
					return formatCompactedToolObservation(toolName, rawResult)
				}
				// Intra-turn rolling scratchpad compaction: on active turn (distance == 0),
				// compact older observations beyond the last 2 tool calls if they exceed 1000 bytes.
				if distance == 0 && totalCalls > 2 && callIdx < totalCalls-2 && len(rawResult) > 1000 {
					return formatCompactedToolObservation(toolName, rawResult)
				}
				if v > 5.0 {
					if fillRatio >= 0.60 && len(rawResult) > 8000 {
						return rawResult[:8000] + "... [truncated]"
					}
					return rawResult
				} else if v > 0.5 {
					if len(rawResult) > 2000 {
						return rawResult[:2000] + "... [truncated]"
					}
					return rawResult
				}
				return formatCompactedToolObservation(toolName, rawResult)
			}

			if len(segments) > 0 {
				for j, seg := range segments {
					var tCalls []domain.ToolCall
					if j < len(node.ToolCalls) {
						tCalls = []domain.ToolCall{node.ToolCalls[j]}
					}

					content := seg.Content
					if m.SignatSteering && j == len(segments)-1 && len(tCalls) == 0 && node.Metadata != nil && node.Metadata["signat"] != "" {
						content = content + " " + node.Metadata["signat"]
					}

					msg := domain.Message{
						Role:     domain.RoleAssistant,
						Content:  content,
						Internal: node.Internal,
					}

					msg.ToolCalls = tCalls
					messages = append(messages, msg)

					for _, tc := range tCalls {
						toolName := tc.Function.Name
						if obs, ok := obsMap[tc.ID]; ok {
							messages = append(messages, domain.Message{
								Role:       domain.RoleTool,
								Content:    formatObs(toolName, obs.Result, j),
								ToolCallID: tc.ID,
								Internal:   node.Internal,
							})
						} else {
							messages = append(messages, domain.Message{
								Role:       domain.RoleTool,
								Content:    fmt.Sprintf("[Tool '%s' execution completed.]", toolName),
								ToolCallID: tc.ID,
								Internal:   node.Internal,
							})
						}
					}
				}

				// Defensive fallback: if there are more ToolCalls than segments,
				// emit them sequentially so the model is never blinded to tool execution results.
				for j := len(segments); j < len(node.ToolCalls); j++ {
					tc := node.ToolCalls[j]
					toolName := tc.Function.Name
					messages = append(messages, domain.Message{
						Role:      domain.RoleAssistant,
						Content:   "",
						Internal:  node.Internal,
						ToolCalls: []domain.ToolCall{tc},
					})

					if obs, ok := obsMap[tc.ID]; ok {
						messages = append(messages, domain.Message{
							Role:       domain.RoleTool,
							Content:    formatObs(toolName, obs.Result, j),
							ToolCallID: tc.ID,
							Internal:   node.Internal,
						})
					} else {
						messages = append(messages, domain.Message{
							Role:       domain.RoleTool,
							Content:    fmt.Sprintf("[Tool '%s' execution completed.]", toolName),
							ToolCallID: tc.ID,
							Internal:   node.Internal,
						})
					}
				}

				continue
			}
		}

		var nodeImages []string
		var metadataText string
		if len(node.Images) > 0 {
			var textParts []string
			for _, imgPath := range node.Images {
				textParts = append(textParts, fmt.Sprintf("[Attached Image: %s]", filepath.Base(imgPath)))
			}
			if len(textParts) > 0 {
				metadataText = "\n\n" + strings.Join(textParts, "\n")
			}
			if supportsVision {
				nodeImages = node.Images
			}
		}

		signatSuffix := ""
		if m.SignatSteering && len(node.ToolCalls) == 0 && node.Metadata != nil && node.Metadata["signat"] != "" && (node.Role == domain.RoleAssistant || node.Role == domain.RoleSystem) {
			signatSuffix = " " + node.Metadata["signat"]
		}

		content := node.Content + signatSuffix + metadataText
		// Layered Genesis Prompt for Ambient Telemetry (ADR 003 Synthesis):
		// Dynamically layer the attentional de-weighting contract onto the root node (messages[0])
		// if ambient_telemetry is enabled, keeping SQLite storage 100% pure persona.
		if m.AmbientTelemetry && (node.Role == domain.RoleSystem || (i == 0 && node.Role != domain.RoleUser)) {
			if !strings.Contains(content, "ADDITIONAL_METADATA") && !strings.Contains(content, "peripheral environmental telemetry") {
				content += "\n\n" + AmbientTelemetryContract
			}
		}

		// Layered Genesis Prompt for Signat Steering (ADR 003 refined):
		// Dynamically layer the signat formatting contract onto the root node (messages[0])
		// if signat_steering is enabled, keeping SQLite storage 100% pure persona.
		if m.SignatSteering && (node.Role == domain.RoleSystem || (i == 0 && node.Role != domain.RoleUser)) {
			if !strings.Contains(content, "signat") && !strings.Contains(content, "emoji signature") {
				content += "\n\n" + SignatSteeringContract
			}
		}

		// Layered Genesis Prompt for Persistent Memory (ADR 014):
		// Dynamically layer active workspace constraints into the root node prefix (<RECALLED_MEMORIES>)
		// to guarantee 100% KV-cache hit rates while protecting the reasoning token budget.
		if node.Role == domain.RoleSystem || (i == 0 && node.Role != domain.RoleUser) {
			if memStore, ok := m.Storage.(storage.MemoryStore); ok && memStore != nil {
				if !strings.Contains(content, "RECALLED_MEMORIES") {
					recalledBlock := m.deriveRecalledMemoriesPrefix(memStore)
					if recalledBlock != "" {
						content += "\n\n" + MemorySteeringContract + "\n\n" + recalledBlock
					}
				}
			}
		}

		// Ephemeral Leaf Telemetry Envelope (ADR 003 Synthesis):
		// Wrap only the active user turn (distance == 0 && RoleUser) in <USER_REQUEST> and <ADDITIONAL_METADATA>.
		// Historical user turns (distance > 0) remain 100% clean, un-bumpered user text.
		if m.AmbientTelemetry && distance == 0 && node.Role == domain.RoleUser {
			telem := DeriveAmbientTelemetry(m.WorkspaceDir, m.clientContext)
			content = FormatTelemetryEnvelope(content, telem)
		}

		msg := domain.Message{
			ID:         node.ID,
			ParentID:   node.ParentID,
			Role:       node.Role,
			Content:    content,
			ToolCallID: node.ToolCallID,
			Internal:   node.Internal,
			Images:     nodeImages,
		}

		if distance >= 1 && len(node.Observations) > 0 {
			// Older turns (distance >= 1): compact large tool observations (ephemeral scratchpad)
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]domain.ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				toolName := "unknown_tool"
				for _, tc := range node.ToolCalls {
					if tc.ID == obs.ToolCallID {
						toolName = tc.Function.Name
						break
					}
				}
				truncatedResult := obs.Result
				if len(truncatedResult) > 1000 {
					truncatedResult = formatCompactedToolObservation(toolName, obs.Result)
				}
				msg.Observations[j] = domain.ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else if v > 5.0 {
			// Keep full fidelity observations, but apply intra-turn rolling compaction for distance == 0
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]domain.ToolObservation, len(node.Observations))
			totalObs := len(node.Observations)
			for j, obs := range node.Observations {
				toolName := "unknown_tool"
				for _, tc := range node.ToolCalls {
					if tc.ID == obs.ToolCallID {
						toolName = tc.Function.Name
						break
					}
				}
				truncatedResult := obs.Result
				if distance == 0 && totalObs > 2 && j < totalObs-2 && len(truncatedResult) > 1000 {
					truncatedResult = formatCompactedToolObservation(toolName, obs.Result)
				} else if fillRatio >= 0.60 && len(truncatedResult) > 8000 {
					truncatedResult = truncatedResult[:8000] + "... [truncated]"
				}
				msg.Observations[j] = domain.ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else if v > 0.5 {
			// Medium fidelity: strip thought, truncate observations to 2000 chars
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]domain.ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				truncatedResult := obs.Result
				if len(truncatedResult) > 2000 {
					truncatedResult = truncatedResult[:2000] + "... [truncated]"
				}
				msg.Observations[j] = domain.ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else {
			// Low fidelity: keep core dialogue, but crush observations with banner retention
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]domain.ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				// Search node.ToolCalls to find tool metadata
				toolName := "unknown_tool"
				for _, tc := range node.ToolCalls {
					if tc.ID == obs.ToolCallID {
						toolName = tc.Function.Name
						break
					}
				}
				msg.Observations[j] = domain.ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     formatCompactedToolObservation(toolName, obs.Result),
				}
			}
		}

		messages = append(messages, msg)

		// Unpack assistant observations as subsequent RoleTool messages for standard provider compliance
		if node.Role == domain.RoleAssistant {
			for _, obs := range msg.Observations {
				messages = append(messages, domain.Message{
					Role:       domain.RoleTool,
					Content:    obs.Result,
					ToolCallID: obs.ToolCallID,
					Internal:   node.Internal,
				})
			}
		}
	}

	return messages, nil
}

// Delegation methods to encapsulated Graph operations

func (m *Manager) GetNode(id string) (*graph.Node, error) {
	return m.Graph.GetNode(id)
}

func (m *Manager) FindNodeByShortID(shortID string) (*graph.Node, error) {
	return m.Graph.FindNodeByShortID(shortID)
}

func (m *Manager) GetPath(nodeID string) ([]*graph.Node, error) {
	return m.Graph.GetPath(nodeID)
}

func (m *Manager) GetChildren(parentID string) []*graph.Node {
	return m.Graph.GetChildren(parentID)
}

func (m *Manager) GetRoots() []*graph.Node {
	return m.Graph.GetRoots()
}

// PruneBranch recursively flags a node and all its descendants as deleted
func (m *Manager) PruneBranch(nodeID string) error {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		// Attempt sync from storage first
		if _, _, syncErr := m.Sync(); syncErr == nil {
			node, err = m.Graph.GetNode(nodeID)
		}
	}
	if err != nil {
		// If the node does not exist in graph or storage, treat as already pruned
		return nil
	}

	// Guard against pruning system root node
	if node.ParentID == "" && node.Role == domain.RoleSystem {
		return fmt.Errorf("cannot prune system root node %s", nodeID)
	}

	// Guard against pruning nodes in active session trajectories
	if m.Storage != nil {
		if sessions, err := m.Storage.ListSessions(); err == nil {
			protected := make(map[string]string)
			for sessName, headID := range sessions {
				if headID == "" {
					continue
				}
				path, err := m.Graph.GetPath(headID)
				if err != nil {
					continue
				}
				for _, pNode := range path {
					protected[pNode.ID] = sessName
				}
			}

			if sessName, ok := protected[nodeID]; ok {
				return fmt.Errorf("cannot prune node %s: node is part of the active trajectory of session %q", nodeID, sessName)
			}
		}
	}

	// Recursive helper to flag and persist
	var flagDeleted func(n *graph.Node) error
	flagDeleted = func(n *graph.Node) error {
		n.Deleted = true
		if err := m.Storage.UpdateNodeMetadata(n); err != nil {
			return err
		}

		children := m.GetChildren(n.ID)
		for _, child := range children {
			if err := flagDeleted(child); err != nil {
				return err
			}
		}
		return nil
	}

	if err := flagDeleted(node); err != nil {
		return err
	}

	// Refresh in-memory graph to reflect deletions
	_, _, err = m.Sync()
	return err
}

// GarbageCollect permanently removes flagged nodes from storage and reloads the graph
func (m *Manager) GarbageCollect() (int64, error) {
	count, err := m.Storage.GarbageCollect()
	if err != nil {
		return count, err
	}

	_, _, err = m.Sync()
	return count, err
}

// CompactRange summarizes a set of nodes and grafts them into the graph as a Supernode
func (m *Manager) CompactRange(ctx context.Context, provider providers.Provider, nodeIDs []string) (*graph.Node, error) {
	return m.CompactRangeWithDirective(ctx, provider, nodeIDs, "")
}

// CompactRangeWithDirective summarizes a set of nodes with an optional user steering directive and grafts them into the graph as a Supernode
func (m *Manager) CompactRangeWithDirective(ctx context.Context, provider providers.Provider, nodeIDs []string, directive string) (*graph.Node, error) {
	if len(nodeIDs) == 0 {
		return nil, fmt.Errorf("no nodes provided for compaction")
	}

	// Guard against compacting ranges containing intermediate active session heads
	if m.Storage != nil && len(nodeIDs) > 1 {
		if sessions, err := m.Storage.ListSessions(); err == nil {
			intermediateMap := make(map[string]bool)
			for _, id := range nodeIDs[:len(nodeIDs)-1] {
				intermediateMap[id] = true
			}
			for sessName, headID := range sessions {
				if intermediateMap[headID] {
					return nil, fmt.Errorf("cannot compact range: contains active session head %s for session %q", headID, sessName)
				}
			}
		}
	}

	var contentToSummarize strings.Builder
	var signats []string
	for _, id := range nodeIDs {
		node, err := m.Graph.GetNode(id)
		if err != nil {
			continue
		}
		signatStr := ""
		if node.Metadata != nil && node.Metadata["signat"] != "" {
			signatStr = " " + node.Metadata["signat"]
			signats = append(signats, strings.TrimSpace(node.Metadata["signat"]))
		}
		fmt.Fprintf(&contentToSummarize, "[%s%s]: %s\n", node.Role, signatStr, node.Content)
	}

	// Build trajectory prefix if signats exist
	trajectoryHeader := ""
	if len(signats) > 0 {
		trajectoryHeader = fmt.Sprintf("🎯 Trajectory: %s\n\n", strings.Join(signats, " ➔ "))
	}

	// 1. Generate Summary and Extract Memories
	summaryPrompt := `You are a concise narrative archivist and knowledge extractor.
Analyze the following conversation segment and produce:
1. SUMMARY: A single, high-density milestone paragraph capturing key facts, architectural decisions, tool results, and the active state of the world. Do not use filler or introductory phrases.
2. MEMORIES: Extract 0-3 enduring facts, technical decisions, or user preferences established in this segment (ignore transient chatter).

Format your response exactly as:
SUMMARY:
<milestone paragraph>

MEMORIES:
- key: <unique_snake_case_key> | category: <architecture|convention|preference|invariant|fact|constraint|workflow> | content: <concise durable fact>

If no enduring memories or decisions exist, output:
MEMORIES:
none`
	if directive != "" {
		summaryPrompt += fmt.Sprintf("\n\nUser Steering Directive: Focus particularly on: %s", directive)
	}

	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: summaryPrompt},
		{Role: domain.RoleUser, Content: contentToSummarize.String()},
	}

	resp, err := provider.GenerateResponse(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// 2. Determine Parentage
	firstNode, err := m.Graph.GetNode(nodeIDs[0]) // Assumes IDs are in chronological order
	if err != nil {
		return nil, fmt.Errorf("failed to find first node in range: %w", err)
	}
	parentID := firstNode.ParentID

	// Get the timestamp from the LAST node to anchor the supernode
	lastNodeID := nodeIDs[len(nodeIDs)-1]
	lastNode, err := m.Graph.GetNode(lastNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find last node in range: %w", err)
	}

	// 3. Parse compaction output and harvest memories
	summary, extractedMemories := parseCompactionOutput(resp.Content, lastNodeID)

	var harvestedKeys []string
	if memStore, ok := m.Storage.(storage.MemoryStore); ok && memStore != nil && len(extractedMemories) > 0 {
		for i := range extractedMemories {
			mem := extractedMemories[i]
			if err := memStore.SaveMemory(&mem); err == nil {
				harvestedKeys = append(harvestedKeys, mem.Key)
			}
		}
	}

	metadata := make(map[string]string)
	if len(harvestedKeys) > 0 {
		metadata["memories_harvested"] = strconv.Itoa(len(harvestedKeys))
		metadata["harvested_memory_keys"] = strings.Join(harvestedKeys, ",")
	}

	// 4. Create Supernode
	superNodeContent := trajectoryHeader + summary
	superNode, err := m.createSupernode(parentID, superNodeContent, lastNode.Timestamp.Add(1*time.Millisecond), metadata)
	if err != nil {
		return nil, err
	}

	// 5. Graft children of the LAST node in the range onto the Supernode
	children := m.Graph.GetChildren(lastNodeID)
	for _, child := range children {
		if err := m.Storage.UpdateNodeParentID(child.ID, superNode.ID); err != nil {
			return nil, fmt.Errorf("failed to re-parent child %s: %w", child.ID, err)
		}
	}

	// 6. Sync to reflect structural changes
	_, _, err = m.Sync()
	return superNode, err
}

func (m *Manager) createSupernode(parentID string, content string, baseTime time.Time, metadata map[string]string) (*graph.Node, error) {
	id, err := newV7FromTime(baseTime)
	if err != nil {
		return nil, fmt.Errorf("failed to generate v7 uuid from time: %w", err)
	}
	node := &graph.Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      domain.RoleSummary,
		Content:   content,
		Timestamp: baseTime,
		Metadata:  metadata,
	}
	if node.Metadata == nil {
		node.Metadata = make(map[string]string)
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist supernode: %w", err)
	}

	return node, nil
}

// parseCompactionOutput separates the dual-yield summary paragraph and any distilled memories.
// If the model generates a legacy unstructured summary, it gracefully treats the entire output as summary.
func parseCompactionOutput(raw string, sourceNodeID string) (string, []storage.Memory) {
	rawTrimmed := strings.TrimSpace(raw)
	if rawTrimmed == "" {
		return "", nil
	}

	upperRaw := strings.ToUpper(raw)
	summaryIdx := strings.Index(upperRaw, "SUMMARY:")
	memoriesIdx := strings.Index(upperRaw, "MEMORIES:")

	// Legacy or unstructured response fallback
	if summaryIdx == -1 {
		return rawTrimmed, nil
	}

	var summaryPart string
	var memoriesPart string

	if memoriesIdx != -1 && memoriesIdx > summaryIdx {
		summaryPart = raw[summaryIdx+len("SUMMARY:") : memoriesIdx]
		memoriesPart = raw[memoriesIdx+len("MEMORIES:"):]
	} else {
		summaryPart = raw[summaryIdx+len("SUMMARY:"):]
	}

	summary := strings.TrimSpace(summaryPart)
	summary = strings.TrimPrefix(summary, "**")
	summary = strings.TrimSuffix(summary, "**")
	summary = strings.TrimSpace(summary)

	if summary == "" {
		summary = rawTrimmed
	}

	if memoriesPart == "" {
		return summary, nil
	}

	var memories []storage.Memory
	seenKeys := make(map[string]bool)

	lines := strings.Split(memoriesPart, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lineTrimmedMarkers := strings.Trim(line, "*_`# \t\r")
		if lineTrimmedMarkers == "" {
			continue
		}
		lowerLine := strings.ToLower(lineTrimmedMarkers)
		if lowerLine == "none" || lowerLine == "none." {
			continue
		}

		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		for i := 1; i <= 9; i++ {
			line = strings.TrimPrefix(line, fmt.Sprintf("%d. ", i))
			line = strings.TrimPrefix(line, fmt.Sprintf("%d) ", i))
		}
		line = strings.TrimSpace(line)

		key, categoryStr, content := parseMemoryLine(line)
		if key == "" || content == "" || strings.ToLower(content) == "none" {
			continue
		}

		if seenKeys[key] {
			continue
		}
		seenKeys[key] = true

		category := normalizeMemoryCategory(categoryStr)
		memID, _ := uuid.NewV7()
		now := time.Now()

		mem := storage.Memory{
			ID:           memID.String(),
			Key:          key,
			Content:      content,
			Category:     category,
			Scope:        storage.ScopeWorkspace,
			Confidence:   0.9,
			SourceNodeID: sourceNodeID,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		memories = append(memories, mem)
	}

	return summary, memories
}

// parseMemoryLine parses key, category, and content from a memory specification line.
func parseMemoryLine(line string) (string, string, string) {
	cleaned := line
	for _, field := range []string{"key", "category", "content", "Key", "Category", "Content"} {
		for _, delim := range []string{
			"**" + field + ":**",
			"**" + field + "**:",
			"*" + field + ":*",
			"*" + field + "*:",
			"`" + field + ":`",
			"`" + field + "`:",
			"_" + field + ":_",
			"_" + field + "_:",
		} {
			cleaned = strings.ReplaceAll(cleaned, delim, strings.ToLower(field)+":")
		}
	}

	var key, category, content string

	if strings.Contains(cleaned, "|") {
		parts := strings.Split(cleaned, "|")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, "*_`")
			part = strings.TrimSpace(part)
			lowerPart := strings.ToLower(part)
			if strings.HasPrefix(lowerPart, "key:") {
				key = strings.TrimSpace(part[4:])
			} else if strings.HasPrefix(lowerPart, "category:") {
				category = strings.TrimSpace(part[9:])
			} else if strings.HasPrefix(lowerPart, "content:") {
				content = strings.TrimSpace(part[8:])
			}
		}
	} else {
		lowerLine := strings.ToLower(cleaned)
		kIdx := strings.Index(lowerLine, "key:")
		cIdx := strings.Index(lowerLine, "category:")
		cntIdx := strings.Index(lowerLine, "content:")

		if kIdx != -1 && cIdx != -1 && cntIdx != -1 {
			if kIdx < cIdx && cIdx < cntIdx {
				key = strings.TrimSpace(cleaned[kIdx+4 : cIdx])
				category = strings.TrimSpace(cleaned[cIdx+9 : cntIdx])
				content = strings.TrimSpace(cleaned[cntIdx+8:])
			}
		}
	}

	key = strings.Trim(key, ",|; \t\"'`")
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, " ", "_")
	var validKey strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			validKey.WriteRune(r)
		}
	}
	key = validKey.String()

	category = strings.Trim(category, ",|; \t\"'`")
	content = strings.Trim(content, " \t\"'`")

	return key, category, content
}

// normalizeMemoryCategory maps textual category names to standard storage.MemoryCategory constants.
func normalizeMemoryCategory(cat string) storage.MemoryCategory {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "architecture", "arch":
		return storage.CategoryArchitecture
	case "preference", "pref":
		return storage.CategoryPreference
	case "constraint", "invariant", "convention":
		return storage.CategoryConstraint
	case "workflow":
		return storage.CategoryWorkflow
	case "scratchpad":
		return storage.CategoryScratchpad
	case "fact":
		return storage.CategoryFact
	default:
		return storage.CategoryFact
	}
}

// newV7FromTime generates a valid UUIDv7 using a specific timestamp
func newV7FromTime(t time.Time) (uuid.UUID, error) {
	var id uuid.UUID

	// Start with a completely random v4 (or random byte slice)
	// We can just call uuid.NewV7() to get a valid v7 with correct variant/version,
	// then just overwrite the 48-bit timestamp.
	var err error
	id, err = uuid.NewV7()
	if err != nil {
		return id, err
	}

	ms := t.UnixMilli()
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)

	return id, nil
}

func (m *Manager) GetSystemRoot() (*graph.Node, error) {
	return m.Graph.GetSystemRoot()
}

func (m *Manager) GetAllNodeIDs() []string {
	ids := make([]string, 0, len(m.Graph.Nodes))
	for id := range m.Graph.Nodes {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) AttachImages(node *graph.Node, images []string) {
	node.Images = images
}

// EstimateContextFill returns the estimated context window fill ratio and token count for a lineage ending at leafID,
// taking into account eager turn-boundary and intra-turn rolling observation compaction.
func (m *Manager) EstimateContextFill(leafID string) (float64, int, error) {
	path, err := m.GetPath(leafID)
	if err != nil {
		return 0, 0, err
	}

	var totalChars int
	for i, node := range path {
		distance := len(path) - 1 - i
		totalChars += len(node.Content) + len(node.Thought)
		totalObs := len(node.Observations)
		for j, obs := range node.Observations {
			obsLen := len(obs.Result)
			if distance >= 1 && obsLen > 1000 {
				obsLen = 150
			} else if distance == 0 && totalObs > 2 && j < totalObs-2 && obsLen > 1000 {
				obsLen = 150
			}
			totalChars += obsLen
		}
	}

	limit := m.NumCtx
	if limit <= 0 {
		limit = 32768
	}
	estimatedTokens := int(float64(totalChars) / 3.8)
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}
	fillRatio := float64(estimatedTokens) / float64(limit)
	return fillRatio, estimatedTokens, nil
}

// deriveRecalledMemoriesPrefix queries active workspace constraints and architecture invariants
// and formats them into a strictly bounded <RECALLED_MEMORIES> XML block for Genesis prefix injection (ADR 014).
func (m *Manager) deriveRecalledMemoriesPrefix(memStore storage.MemoryStore) string {
	mems, err := memStore.QueryMemories(storage.MemoryFilter{
		Limit: 15,
	})
	if err != nil || len(mems) == 0 {
		return ""
	}

	var filtered []storage.Memory
	for _, mem := range mems {
		if (mem.Scope == storage.ScopeWorkspace || mem.Scope == storage.ScopeGlobal) &&
			(mem.Category == storage.CategoryConstraint || mem.Category == storage.CategoryArchitecture) {
			filtered = append(filtered, mem)
		}
	}
	if len(filtered) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<RECALLED_MEMORIES count=\"%d\">\n", len(filtered)))
	totalChars := 0
	const maxChars = 2000 // Strict 300-500 token budget cap

	for _, f := range filtered {
		item := fmt.Sprintf("  <memory key=%q scope=%q category=%q>%s</memory>\n", f.Key, string(f.Scope), string(f.Category), strings.TrimSpace(f.Content))
		if totalChars+len(item) > maxChars {
			break
		}
		sb.WriteString(item)
		totalChars += len(item)
	}
	sb.WriteString("</RECALLED_MEMORIES>")
	return sb.String()
}
