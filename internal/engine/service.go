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
	Graph        *Graph
	Storage      Storage
	Registry     *ToolRegistry
	WorkspaceDir string
	NumCtx       int
}

// NewManager creates a new Manager instance
func NewManager(g *Graph, s Storage) *Manager {
	return &Manager{
		Graph:        g,
		Storage:      s,
		Registry:     NewToolRegistry(),
		WorkspaceDir: ".",
		NumCtx:       32768,
	}
}

// CreateNode handles the full lifecycle of creating a new node:
// ID generation, graph insertion, and persistence.
func (m *Manager) CreateNode(parentID string, role Role, content string, internal bool) (*Node, error) {
	id, _ := uuid.NewV7()
	cleanContent, signat := ExtractSignat(content)

	node := &Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Internal:  internal,
		Metadata:  make(map[string]string),
	}

	if signat != "" && (role == RoleSystem || role == RoleSummary) {
		node.Metadata["signat"] = signat
		node.Content = cleanContent
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
	cleanContent, signat := ExtractSignat(content)

	node := &Node{
		ID:        id.String(),
		ParentID:  parentID,
		Role:      RoleAssistant,
		Content:   cleanContent,
		Thought:   thought,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
		Internal:  internal,
		Metadata:  make(map[string]string),
	}

	if signat != "" {
		node.Metadata["signat"] = signat
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
func (m *Manager) calculateResonanceScore(node *Node, distance int, fillRatio float64, totalPathLen int) float64 {
	if node.Role == RoleSystem || node.Role == RoleSummary {
		return math.MaxFloat64
	}

	// Under 60% context capacity, keep 100% full fidelity across all public historical nodes
	if fillRatio < 0.60 && !node.Internal {
		return 100.0
	}

	weight := 0.7
	switch node.Role {
	case RoleUser:
		weight = 1.0
	case RoleTool:
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

// BuildLLMContext constructs the message history for the LLM, applying Priority Pruning based on the Context Resonance Score.
func (m *Manager) BuildLLMContext(leafID string, supportsVision bool) ([]Message, error) {
	path, err := m.GetPath(leafID)
	if err != nil {
		return nil, err
	}

	// Calculate total path cost to determine fill ratio
	var totalChars int
	for _, node := range path {
		totalChars += len(node.Content) + len(node.Thought)
		for _, obs := range node.Observations {
			totalChars += len(obs.Result)
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

	var messages []Message
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

					content := seg.Content
					if j == len(segments)-1 && node.Metadata != nil && node.Metadata["signat"] != "" {
						content = content + " " + node.Metadata["signat"]
					}

					msg := Message{
						Role:     RoleAssistant,
						Content:  content,
						Internal: node.Internal,
					}

					msg.ToolCalls = tCalls
					messages = append(messages, msg)

					if j < len(node.ToolCalls) && j < len(node.Observations) {
						obs := node.Observations[j]
						truncatedResult := obs.Result
						toolName := node.ToolCalls[j].Function.Name
						if distance >= 2 && len(truncatedResult) > 1000 {
							truncatedResult = fmt.Sprintf("[Tool '%s' execution completed. Detailed results omitted. Total size: %d bytes.]", toolName, len(obs.Result))
						} else if v > 5.0 {
							if fillRatio >= 0.60 && len(truncatedResult) > 8000 {
								truncatedResult = truncatedResult[:8000] + "... [truncated]"
							}
						} else if v > 0.5 {
							if len(truncatedResult) > 2000 {
								truncatedResult = truncatedResult[:2000] + "... [truncated]"
							}
						} else {
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

		signatSuffix := ""
		if node.Metadata != nil && node.Metadata["signat"] != "" && (node.Role == RoleAssistant || node.Role == RoleSystem) {
			signatSuffix = " " + node.Metadata["signat"]
		}

		msg := Message{
			ID:         node.ID,
			ParentID:   node.ParentID,
			Role:       node.Role,
			Content:    node.Content + signatSuffix + metadataText,
			ToolCallID: node.ToolCallID,
			Internal:   node.Internal,
			Images:     nodeImages,
		}

		if distance >= 2 && len(node.Observations) > 0 {
			// Older turns (distance >= 2): compact large tool observations (ephemeral scratchpad)
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]ToolObservation, len(node.Observations))
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
					truncatedResult = fmt.Sprintf("[Tool '%s' execution completed. Detailed results omitted. Total size: %d bytes.]", toolName, len(obs.Result))
				}
				msg.Observations[j] = ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else if v > 5.0 {
			// Keep full fidelity observations
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				truncatedResult := obs.Result
				if fillRatio >= 0.60 && len(truncatedResult) > 8000 {
					truncatedResult = truncatedResult[:8000] + "... [truncated]"
				}
				msg.Observations[j] = ToolObservation{
					ToolCallID: obs.ToolCallID,
					Result:     truncatedResult,
				}
			}
		} else if v > 0.5 {
			// Medium fidelity: strip thought, truncate observations to 2000 chars
			msg.ToolCalls = node.ToolCalls
			msg.Observations = make([]ToolObservation, len(node.Observations))
			for j, obs := range node.Observations {
				truncatedResult := obs.Result
				if len(truncatedResult) > 2000 {
					truncatedResult = truncatedResult[:2000] + "... [truncated]"
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

		// Unpack assistant observations as subsequent RoleTool messages for standard provider compliance
		if node.Role == RoleAssistant {
			for _, obs := range msg.Observations {
				messages = append(messages, Message{
					Role:       RoleTool,
					Content:    obs.Result,
					ToolCallID: obs.ToolCallID,
					Internal:   node.Internal,
				})
			}
		}
	}

	// Project ephemeral workspace telemetry onto the active leaf turn (ADR 003)
	// Only project when the target leaf node being evaluated is an active user turn.
	if len(path) > 0 && path[len(path)-1].Role == RoleUser {
		leafUserID := path[len(path)-1].ID
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == RoleUser && messages[i].ID == leafUserID {
				messages[i].Content += m.GenerateWorkspaceSupplement()
				break
			}
		}
	}

	return messages, nil
}

// GenerateWorkspaceSupplement constructs the ephemeral grounding telemetry for the active turn.
// Gathers current directory listing (skipping hidden files), index.md header, and concise tool execution guidelines.
func (m *Manager) GenerateWorkspaceSupplement() string {
	wsDir := m.WorkspaceDir
	if wsDir == "" {
		wsDir = "."
	}

	var sb strings.Builder
	sb.WriteString("\n\n### ACTIVE WORKSPACE TELEMETRY\n")

	// 1. Shallow Directory Listing (skipping hidden files/directories)
	entries, err := os.ReadDir(wsDir)
	if err == nil {
		sb.WriteString("Current Directory Tree:\n")
		count := 0
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue // Skip hidden files/dirs like .git
			}
			if e.IsDir() {
				sb.WriteString(fmt.Sprintf(" - %s/\n", name))
			} else {
				sb.WriteString(fmt.Sprintf(" - %s\n", name))
			}
			count++
			if count >= 30 {
				sb.WriteString(" - ... [additional files omitted]\n")
				break
			}
		}
	}

	// 2. Index Header
	indexPath := filepath.Join(wsDir, "index.md")
	if indexContent, err := os.ReadFile(indexPath); err == nil {
		lines := strings.Split(string(indexContent), "\n")
		limit := 25
		if len(lines) < limit {
			limit = len(lines)
		}
		sb.WriteString("\nIndex Snippet:\n")
		sb.WriteString(strings.Join(lines[:limit], "\n"))
		sb.WriteString("\n")
	}

	// 3. Tool Execution Guidelines
	sb.WriteString("\n### TOOL GUIDELINES\n")
	sb.WriteString("- Cadence: Inspect (read/grep/list) -> Mutate (edit/append/write) -> Verify.\n")
	sb.WriteString("- Retention: Tool observations decay; state essential paths, byte sizes, and findings in your dialogue.\n")
	sb.WriteString("- Mutation: Use `edit_file` for in-place edits, `append_file` for logs/notes, and `write_file` for new files (`overwrite: true` to clobber).\n")
	sb.WriteString("- Thinking: Use <think> exclusively for immediate step planning and output verification.\n")

	return sb.String()
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
		// Attempt sync from storage first
		if _, _, syncErr := m.Sync(); syncErr == nil {
			node, err = m.Graph.GetNode(nodeID)
		}
	}
	if err != nil {
		// If the node does not exist in graph or storage, treat as already pruned
		return nil
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
	return m.CompactRangeWithDirective(ctx, provider, nodeIDs, "")
}

// CompactRangeWithDirective summarizes a set of nodes with an optional user steering directive and grafts them into the graph as a Supernode
func (m *Manager) CompactRangeWithDirective(ctx context.Context, provider LLMProvider, nodeIDs []string, directive string) (*Node, error) {
	if len(nodeIDs) == 0 {
		return nil, fmt.Errorf("no nodes provided for compaction")
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

	// 1. Generate Summary
	summaryPrompt := "You are a concise narrative archivist. Summarize the following conversation segment into a single, high-density milestone paragraph. Preserve key facts, architectural decisions, tool results, and the active state of the world. Do not use filler or introductory phrases."
	if directive != "" {
		summaryPrompt += fmt.Sprintf("\nUser Steering Directive: Focus particularly on: %s", directive)
	}

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
	superNodeContent := trajectoryHeader + strings.TrimSpace(resp.Content)
	superNode, err := m.createSupernode(parentID, superNodeContent, lastNode.Timestamp.Add(1*time.Millisecond))
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
