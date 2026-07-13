// package engine provides the core business logic for the Please application,
// including DAG management, LLM provider integration, and state persistence.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"os"
	"path/filepath"

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

// Manager is the central coordinator for the application engine. It provides
// a high-level API that combines graph operations (traversal, branching)
// with storage persistence, ensuring that all narrative changes are saved.
type Manager struct {
	Graph    *Graph
	Storage  Storage
	Registry *ToolRegistry
}

// NewManager creates a new Manager instance
func NewManager(g *Graph, s Storage) *Manager {
	return &Manager{
		Graph:    g,
		Storage:  s,
		Registry: NewToolRegistry(),
	}
}

// CreateNode handles the full lifecycle of creating a new node:
// ID generation, graph insertion, and persistence.
func (m *Manager) CreateNode(parentID string, role Role, content string, internal bool) (*Node, error) {
	id, _ := uuid.NewV7()
	node := &Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Internal:  internal,
	}

	if role == RoleTool && node.ToolCallID == "" {
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
func (m *Manager) CreateAssistantNode(parentID string, content string, thought string, toolCalls []ToolCall, internal bool) (*Node, error) {
	id, _ := uuid.NewV7()
	node := &Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      RoleAssistant,
		Content:   content,
		Thought:   thought,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
		Internal:  internal,
		Metadata:  make(map[string]string),
	}

	segments := []AssistantSegment{
		{
			Content: content,
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

// CreateToolNode creates a node containing the result of a tool execution
func (m *Manager) CreateToolNode(parentID string, toolCallID string, content string, internal bool) (*Node, error) {
	id, _ := uuid.NewV7()
	node := &Node{
		ID:         id.String(),
		ParentID:   parentID,
		Role:       RoleTool,
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

	if node.Role != RoleAssistant {
		return fmt.Errorf("observations can only be added to assistant nodes")
	}

	node.Observations = append(node.Observations, ToolObservation{
		ToolCallID: callID,
		Result:     result,
	})

	return m.Storage.UpdateNodeObservations(nodeID, node.Observations)
}

func (m *Manager) validateNode(node *Node) error {
	if node.ID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}
	if node.ID == node.ParentID {
		return fmt.Errorf("node cannot be its own parent (cycle detected)")
	}
	if node.Role == RoleUser && strings.TrimSpace(node.Content) == "" {
		return fmt.Errorf("user message content cannot be empty")
	}
	if node.Role == RoleTool {
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
func (m *Manager) ExecuteToolCall(ctx context.Context, call ToolCall) (string, error) {
	tool, ok := m.Registry.Tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", call.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	return tool.Function(ctx, args)
}

// Sync reloads the graph from storage, effectively synchronizing the in-memory state
// with any external changes (e.g., from other 'please' sessions).
func (m *Manager) Sync() (*Graph, string, error) {
	graph, lastID, err := m.Storage.LoadGraph()
	if err != nil {
		return nil, "", err
	}
	m.Graph = graph
	return graph, lastID, nil
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
func (m *Manager) calculateResonanceScore(node *Node, distance int) float64 {
	if node.Role == RoleSystem || node.Role == RoleSummary {
		return math.MaxFloat64
	}

	weight := 0.7
	switch node.Role {
	case RoleUser:
		weight = 1.0
	case RoleTool:
		weight = 0.5
	}
	
	if node.Internal {
		weight = 0.3
	}
	if node.Metadata != nil && node.Metadata["bookmarked"] == "true" {
		weight = 1.0
	}

	cost := len(node.Content) + len(node.Thought)
	for _, obs := range node.Observations {
		cost += len(obs.Result)
	}
	if cost == 0 {
		cost = 1
	}

	baseScore := weight * 10000.0 / float64(cost)

	// Grace Window: last 3 turns are exempt from time/turn decay
	const GraceTurns = 3
	if distance < GraceTurns {
		return baseScore
	}

	deltaMinutes := time.Since(node.Timestamp).Minutes()
	if deltaMinutes < 0 {
		deltaMinutes = 0
	}

	// Hybrid Decay Model:
	// - Slower temporal decay (half-life of ~35 minutes, k_t = 0.02)
	// - Conversational turn decay (k_d = 0.1 per turn beyond the grace window)
	kt := 0.02
	kd := 0.1
	turnsPastGrace := float64(distance - GraceTurns + 1)

	decayFactor := math.Exp(-kt*deltaMinutes) * math.Exp(-kd*turnsPastGrace)
	return baseScore * decayFactor
}

// BuildLLMContext constructs the message history for the LLM, applying Priority Pruning based on the Context Resonance Score.
func (m *Manager) BuildLLMContext(leafID string, supportsVision bool) ([]Message, error) {
	path, err := m.GetPath(leafID)
	if err != nil {
		return nil, err
	}

	var messages []Message
	for i, node := range path {
		distance := len(path) - 1 - i
		v := m.calculateResonanceScore(node, distance)
		
		// The active/latest node should always be kept in high fidelity regardless of score
		if distance == 0 {
			v = math.MaxFloat64
		}

		if node.Internal && v <= 0.5 {
			continue // Drop low fidelity internal nodes entirely
		}

		if node.Role == RoleAssistant {
			var segments []AssistantSegment
			if node.Metadata != nil && node.Metadata["segments"] != "" {
				_ = json.Unmarshal([]byte(node.Metadata["segments"]), &segments)
			}

			if len(segments) > 0 {
				for j, seg := range segments {
					var tCalls []ToolCall
					if j < len(node.ToolCalls) {
						tCalls = []ToolCall{node.ToolCalls[j]}
					}

					msg := Message{
						Role:     RoleAssistant,
						Content:  seg.Content,
						Internal: node.Internal,
					}

					if v > 5.0 {
						msg.Thought = seg.Thought
						msg.ToolCalls = tCalls
					} else if v > 0.5 {
						msg.ToolCalls = tCalls
					} else {
						msg.ToolCalls = tCalls
					}

					messages = append(messages, msg)

					if j < len(node.ToolCalls) && j < len(node.Observations) {
						obs := node.Observations[j]
						truncatedResult := obs.Result
						if v > 5.0 {
							if len(truncatedResult) > 4000 {
								truncatedResult = truncatedResult[:4000] + "... [truncated]"
							}
						} else if v > 0.5 {
							if len(truncatedResult) > 500 {
								truncatedResult = truncatedResult[:500] + "... [truncated]"
							}
						} else {
							toolName := node.ToolCalls[j].Function.Name
							truncatedResult = fmt.Sprintf("[Tool '%s' execution completed. Detailed results omitted. Total size: %d bytes.]", toolName, len(obs.Result))
						}

						messages = append(messages, Message{
							Role:       RoleTool,
							Content:    truncatedResult,
							ToolCallID: obs.ToolCallID,
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
				file, err := os.Open(imgPath)
				var sdDetails map[string]string
				if err == nil {
					meta, err := ExtractPNGMetadata(file)
					if err == nil {
						if params, exists := meta["parameters"]; exists {
							sdDetails = ParseSDParameters(params)
						}
					}
					file.Close()
				}
				if sdDetails != nil && sdDetails["sd_prompt"] != "" {
					textParts = append(textParts, fmt.Sprintf("[Attached Image: %s | SD Prompt: %s | Seed: %s | Model: %s]", filepath.Base(imgPath), sdDetails["sd_prompt"], sdDetails["sd_seed"], sdDetails["sd_model"]))
				} else {
					textParts = append(textParts, fmt.Sprintf("[Attached Image: %s (No SD metadata available)]", filepath.Base(imgPath)))
				}
			}
			if len(textParts) > 0 {
				metadataText = "\n\n" + strings.Join(textParts, "\n")
			}
			if supportsVision {
				nodeImages = node.Images
			}
		}

		msg := Message{
			Role:       node.Role,
			Content:    node.Content + metadataText,
			ToolCallID: node.ToolCallID,
			Internal:   node.Internal,
			Images:     nodeImages,
		}

		if v > 5.0 {
			// Keep full fidelity
			msg.Thought = node.Thought
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				truncatedResult := obs.Result
				if len(truncatedResult) > 4000 {
					truncatedResult = truncatedResult[:4000] + "... [truncated]"
				}
				msg.Observations[j] = ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else if v > 0.5 {
			// Medium fidelity: strip thought, truncate observations more aggressively
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				truncatedResult := obs.Result
				if len(truncatedResult) > 500 {
					truncatedResult = truncatedResult[:500] + "... [truncated]"
				}
				msg.Observations[j] = ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else {
			// Low fidelity: keep core dialogue, but crush observations
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				// Search node.ToolCalls to find tool metadata
				toolName := "unknown_tool"
				for _, tc := range node.ToolCalls {
					if tc.ID == obs.ToolCallID {
						toolName = tc.Function.Name
						break
					}
				}
				msg.Observations[j] = ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     fmt.Sprintf("[Tool '%s' execution completed. Detailed results omitted. Total size: %d bytes.]", toolName, len(obs.Result)),
				}
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// Delegation methods to encapsulated Graph operations

func (m *Manager) GetNode(id string) (*Node, error) {
	return m.Graph.GetNode(id)
}

func (m *Manager) FindNodeByShortID(shortID string) (*Node, error) {
	return m.Graph.FindNodeByShortID(shortID)
}

func (m *Manager) GetPath(nodeID string) ([]*Node, error) {
	return m.Graph.GetPath(nodeID)
}

func (m *Manager) GetChildren(parentID string) []*Node {
	return m.Graph.GetChildren(parentID)
}

func (m *Manager) GetRoots() []*Node {
	return m.Graph.GetRoots()
}

// PruneBranch recursively flags a node and all its descendants as deleted
func (m *Manager) PruneBranch(nodeID string) error {
	node, err := m.Graph.GetNode(nodeID)
	if err != nil {
		return err
	}

	// Recursive helper to flag and persist
	var flagDeleted func(n *Node) error
	flagDeleted = func(n *Node) error {
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
func (m *Manager) CompactRange(ctx context.Context, provider LLMProvider, nodeIDs []string) (*Node, error) {
	if len(nodeIDs) == 0 {
		return nil, fmt.Errorf("no nodes provided for compaction")
	}

	var contentToSummarize strings.Builder
	for _, id := range nodeIDs {
		node, err := m.Graph.GetNode(id)
		if err != nil {
			continue
		}
		fmt.Fprintf(&contentToSummarize, "[%s]: %s\n", node.Role, node.Content)
	}

	// 1. Generate Summary
	summaryPrompt := "You are a concise narrative archivist. Summarize the following conversation segment into a single, high-density paragraph. Preserve key facts, decisions, and the current state of the world. Do not use filler or introductory phrases."
	messages := []Message{
		{Role: RoleSystem, Content: summaryPrompt},
		{Role: RoleUser, Content: contentToSummarize.String()},
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

	// 3. Create Supernode
	// We add 1 millisecond to ensure strict monotonicity. If it shared the exact same ms, 
	// a lower random tail would cause the Supernode to sort *before* the last node lexically.
	superNode, err := m.createSupernode(parentID, resp.Content, lastNode.Timestamp.Add(1*time.Millisecond))
	if err != nil {
		return nil, err
	}

	// 4. Graft children of the LAST node in the range onto the Supernode
	children := m.Graph.GetChildren(lastNodeID)
	for _, child := range children {
		if err := m.Storage.UpdateNodeParentID(child.ID, superNode.ID); err != nil {
			return nil, fmt.Errorf("failed to re-parent child %s: %w", child.ID, err)
		}
	}

	// 5. Sync to reflect structural changes
	_, _, err = m.Sync()
	return superNode, err
}

func (m *Manager) createSupernode(parentID string, content string, baseTime time.Time) (*Node, error) {
	id, err := newV7FromTime(baseTime)
	if err != nil {
		return nil, fmt.Errorf("failed to generate v7 uuid from time: %w", err)
	}
	node := &Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      RoleSummary,
		Content:   content,
		Timestamp: baseTime,
	}

	m.Graph.AddNode(node)
	if err := m.Storage.SaveNode(node); err != nil {
		return nil, fmt.Errorf("failed to persist supernode: %w", err)
	}

	return node, nil
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

func (m *Manager) GetSystemRoot() (*Node, error) {
	return m.Graph.GetSystemRoot()
}

func (m *Manager) GetAllNodeIDs() []string {
	ids := make([]string, 0, len(m.Graph.Nodes))
	for id := range m.Graph.Nodes {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) AttachImages(node *Node, images []string) {
	node.Images = images
	if node.Metadata == nil {
		node.Metadata = make(map[string]string)
	}
	for _, imgPath := range images {
		file, err := os.Open(imgPath)
		if err == nil {
			meta, err := ExtractPNGMetadata(file)
			if err == nil {
				if params, exists := meta["parameters"]; exists {
					sdDetails := ParseSDParameters(params)
					for k, v := range sdDetails {
						node.Metadata[k] = v
					}
				}
			}
			file.Close()
		}
	}
}
