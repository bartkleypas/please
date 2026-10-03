package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/storage"
)

// ContextShaper formats and filters an active DAG path into a cache-stable prompt sequence.
// Pure, read-only projection: DAG Path x Budget -> []domain.Message with zero graph or storage mutations.
type ContextShaper interface {
	ShapeContext(ctx context.Context, path []*graph.Node, budget int) ([]domain.Message, error)
}

// FidelityTier represents the retention fidelity for a node in prompt projection.
type FidelityTier int

const (
	FidelityFull FidelityTier = iota
	FidelityMedium
	FidelityLow
	FidelityDrop
)

// ShapeOptions holds contextual execution parameters for prompt projection.
type ShapeOptions struct {
	SupportsVision   bool
	SignatSteering   bool
	AmbientTelemetry bool
	WorkspaceDir     string
	ClientContext    map[string]string
	MemoryStore      storage.MemoryStore
}

type shapeOptionsKey struct{}

// WithShapeOptions attaches ShapeOptions to a context.
func WithShapeOptions(ctx context.Context, opts ShapeOptions) context.Context {
	return context.WithValue(ctx, shapeOptionsKey{}, opts)
}

// ShapeOptionsFromContext retrieves ShapeOptions from a context, or returns false if not set.
func ShapeOptionsFromContext(ctx context.Context) (ShapeOptions, bool) {
	if ctx == nil {
		return ShapeOptions{}, false
	}
	opts, ok := ctx.Value(shapeOptionsKey{}).(ShapeOptions)
	return opts, ok
}

// SigmoidShaper applies a logistic S-curve to conversational distance:
// S(d) = 1 / (1 + e^(k*(d - d0)))
// Provides an active plateau for recent turns, a smooth roll-off, and a stable non-zero floor.
type SigmoidShaper struct {
	Options ShapeOptions
	K       float64 // slope factor (default 0.8)
	D0      float64 // inflection distance (default 6.0)
}

// NewSigmoidShaper creates an initialized SigmoidShaper.
func NewSigmoidShaper(opts ...func(*SigmoidShaper)) *SigmoidShaper {
	s := &SigmoidShaper{
		K:  0.8,
		D0: 6.0,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ShapeContext implements ContextShaper for SigmoidShaper.
func (s *SigmoidShaper) ShapeContext(ctx context.Context, path []*graph.Node, budget int) ([]domain.Message, error) {
	effectiveOpts := s.Options
	if ctxOpts, ok := ShapeOptionsFromContext(ctx); ok {
		effectiveOpts = ctxOpts
	}

	return projectPathToMessages(path, budget, effectiveOpts, func(node *graph.Node, idx int, distance int, fillRatio float64) FidelityTier {
		// Monotonic Prefix Invariance: Genesis Root & initial user goal are permanently pinned
		if idx == 0 || (idx == 1 && node.Role == domain.RoleUser) {
			return FidelityFull
		}
		// Active leaf is always full fidelity
		if distance == 0 {
			return FidelityFull
		}

		// Capacity Pressure Check: Under 60% capacity, all public nodes retain full fidelity
		if fillRatio < 0.60 && !node.Internal {
			return FidelityFull
		}

		// Compute logistic retention value
		d := float64(distance)
		sVal := 1.0 / (1.0 + math.Exp(s.K*(d-s.D0)))

		if node.Internal {
			cost := len(node.Content) + len(node.Thought)
			for _, obs := range node.Observations {
				cost += len(obs.Result)
			}
			if cost == 0 {
				cost = 1
			}
			internalScore := (0.05 * 1000.0 / float64(cost)) * sVal
			if internalScore <= 0.5 {
				return FidelityDrop
			}
			return FidelityMedium
		}

		if sVal >= 0.5 {
			return FidelityFull
		} else if sVal >= 0.15 {
			return FidelityMedium
		}
		// Stable Floor / Frozen Asymptote: Settles onto discrete low-fidelity baseline
		return FidelityLow
	})
}

// WindowShaper enforces a strict K-turn sliding window + pinned Genesis root.
// Zero-math, perfectly deterministic boundary for tight context windows.
type WindowShaper struct {
	Options    ShapeOptions
	WindowSize int // default: 10
}

// NewWindowShaper creates an initialized WindowShaper.
func NewWindowShaper(windowSize int, opts ...func(*WindowShaper)) *WindowShaper {
	if windowSize <= 0 {
		windowSize = 10
	}
	s := &WindowShaper{
		WindowSize: windowSize,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ShapeContext implements ContextShaper for WindowShaper.
func (s *WindowShaper) ShapeContext(ctx context.Context, path []*graph.Node, budget int) ([]domain.Message, error) {
	effectiveOpts := s.Options
	if ctxOpts, ok := ShapeOptionsFromContext(ctx); ok {
		effectiveOpts = ctxOpts
	}

	return projectPathToMessages(path, budget, effectiveOpts, func(node *graph.Node, idx int, distance int, fillRatio float64) FidelityTier {
		// Monotonic Prefix Invariance: Genesis Root & initial user goal are permanently pinned
		if idx == 0 || (idx == 1 && node.Role == domain.RoleUser) {
			return FidelityFull
		}
		if distance == 0 {
			return FidelityFull
		}

		if fillRatio < 0.60 && !node.Internal {
			return FidelityFull
		}

		if distance < s.WindowSize {
			if node.Internal {
				cost := len(node.Content) + len(node.Thought)
				for _, obs := range node.Observations {
					cost += len(obs.Result)
				}
				if cost > 500 {
					return FidelityDrop
				}
				return FidelityMedium
			}
			return FidelityFull
		}

		// Historical turns outside window
		if node.Internal {
			return FidelityDrop
		}
		return FidelityLow
	})
}

// ResonanceShaper refactors the Context Resonance formula into a pure topological decay
// based on turn distance Δd, completely eliminating wall-clock time.Since decay.
type ResonanceShaper struct {
	Options ShapeOptions
}

// NewResonanceShaper creates an initialized ResonanceShaper.
func NewResonanceShaper(opts ...func(*ResonanceShaper)) *ResonanceShaper {
	s := &ResonanceShaper{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ShapeContext implements ContextShaper for ResonanceShaper.
func (s *ResonanceShaper) ShapeContext(ctx context.Context, path []*graph.Node, budget int) ([]domain.Message, error) {
	effectiveOpts := s.Options
	if ctxOpts, ok := ShapeOptionsFromContext(ctx); ok {
		effectiveOpts = ctxOpts
	}

	return projectPathToMessages(path, budget, effectiveOpts, func(node *graph.Node, idx int, distance int, fillRatio float64) FidelityTier {
		if idx == 0 || (idx == 1 && node.Role == domain.RoleUser) {
			return FidelityFull
		}
		if distance == 0 {
			return FidelityFull
		}

		score := CalculateTopologicalResonanceScore(node, distance, fillRatio, len(path))
		if node.Internal && score <= 0.5 {
			return FidelityDrop
		}
		if score > 5.0 {
			return FidelityFull
		} else if score > 0.5 {
			return FidelityMedium
		}
		return FidelityLow
	})
}

// CalculateTopologicalResonanceScore determines the context value of a node based on
// topological weight, compute cost, and turn distance Δd. Wall-clock decay is retired.
func CalculateTopologicalResonanceScore(node *graph.Node, distance int, fillRatio float64, totalPathLen int) float64 {
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
	kd := 0.3

	if fillRatio < 0.85 {
		// Moderate load (60% - 85%): expand grace turns and slow decay rate
		graceTurns = int(float64(totalPathLen) * 0.5)
		if graceTurns < 5 {
			graceTurns = 5
		}
		kd = 0.1
	}

	if distance < graceTurns {
		return baseScore
	}

	turnsPastGrace := float64(distance - graceTurns + 1)
	decayFactor := math.Exp(-kd * turnsPastGrace)
	return baseScore * decayFactor
}

// NewContextShaper constructs a ContextShaper based on the specified curve type.
// Supported curves: "sigmoid" (default), "window", "resonance" (or "exponential").
func NewContextShaper(shaperType string, opts ShapeOptions) ContextShaper {
	switch strings.ToLower(strings.TrimSpace(shaperType)) {
	case "window":
		w := NewWindowShaper(10)
		w.Options = opts
		return w
	case "resonance", "exponential":
		r := NewResonanceShaper()
		r.Options = opts
		return r
	case "sigmoid", "":
		fallthrough
	default:
		s := NewSigmoidShaper()
		s.Options = opts
		return s
	}
}

// projectPathToMessages is the pure, deterministic projection pipeline transforming a DAG path
// into a sequence of domain.Message models adhering to Monotonic Prefix Invariance.
func projectPathToMessages(
	path []*graph.Node,
	budget int,
	opts ShapeOptions,
	tierFunc func(node *graph.Node, idx int, distance int, fillRatio float64) FidelityTier,
) ([]domain.Message, error) {
	if len(path) == 0 {
		return nil, nil
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

	limit := budget
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
		tier := tierFunc(node, i, distance, fillRatio)

		if tier == FidelityDrop {
			continue
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
				if tier == FidelityFull {
					if fillRatio >= 0.60 && len(rawResult) > 8000 {
						return rawResult[:8000] + "... [truncated]"
					}
					return rawResult
				} else if tier == FidelityMedium {
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
					if opts.SignatSteering && j == len(segments)-1 && len(tCalls) == 0 && node.Metadata != nil && node.Metadata["signat"] != "" {
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
			if opts.SupportsVision {
				nodeImages = node.Images
			}
		}

		signatSuffix := ""
		if opts.SignatSteering && len(node.ToolCalls) == 0 && node.Metadata != nil && node.Metadata["signat"] != "" && (node.Role == domain.RoleAssistant || node.Role == domain.RoleSystem) {
			signatSuffix = " " + node.Metadata["signat"]
		}

		content := node.Content + signatSuffix + metadataText
		// Layered Genesis Prompt for Ambient Telemetry (ADR 003 Synthesis):
		// Dynamically layer the attentional de-weighting contract onto the root node (messages[0])
		// if ambient_telemetry is enabled, keeping SQLite storage 100% pure persona.
		if opts.AmbientTelemetry && (node.Role == domain.RoleSystem || (i == 0 && node.Role != domain.RoleUser)) {
			if !strings.Contains(content, "ADDITIONAL_METADATA") && !strings.Contains(content, "peripheral environmental telemetry") {
				content += "\n\n" + AmbientTelemetryContract
			}
		}

		// Layered Genesis Prompt for Signat Steering (ADR 003 refined):
		// Dynamically layer the signat formatting contract onto the root node (messages[0])
		// if signat_steering is enabled, keeping SQLite storage 100% pure persona.
		if opts.SignatSteering && (node.Role == domain.RoleSystem || (i == 0 && node.Role != domain.RoleUser)) {
			if !strings.Contains(content, "signat") && !strings.Contains(content, "emoji signature") {
				content += "\n\n" + SignatSteeringContract
			}
		}

		// Layered Genesis Prompt for Persistent Memory (ADR 014):
		// Dynamically layer active workspace constraints into the root node prefix (<RECALLED_MEMORIES>)
		// to guarantee 100% KV-cache hit rates while protecting the reasoning token budget.
		if node.Role == domain.RoleSystem || (i == 0 && node.Role != domain.RoleUser) {
			if opts.MemoryStore != nil {
				if !strings.Contains(content, "RECALLED_MEMORIES") {
					recalledBlock := DeriveRecalledMemoriesPrefix(opts.MemoryStore)
					if recalledBlock != "" {
						content += "\n\n" + MemorySteeringContract + "\n\n" + recalledBlock
					}
				}
			}
		}

		// Ephemeral Leaf Telemetry Envelope (ADR 003 Synthesis):
		// Wrap only the active user turn (distance == 0 && RoleUser) in <USER_REQUEST> and <ADDITIONAL_METADATA>.
		// Historical user turns (distance > 0) remain 100% clean, un-bumpered user text.
		if opts.AmbientTelemetry && distance == 0 && node.Role == domain.RoleUser {
			telem := DeriveAmbientTelemetry(opts.WorkspaceDir, opts.ClientContext)
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
		} else if tier == FidelityFull {
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
		} else if tier == FidelityMedium {
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
