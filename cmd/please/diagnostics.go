package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/engine"
)

func setupDiagnosticsManager(vaultPath, configPath string) (*engine.Manager, *config.Config, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.NewDefaultConfig()
	}
	if configPath != "" {
		fileCfg, err := config.LoadConfigFile(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load config %q: %w", configPath, err)
		}
		cfg = fileCfg
	}

	finalVaultPath := ""
	if cfg.Server != nil && cfg.Server.VaultPath != "" {
		finalVaultPath = cfg.Server.VaultPath
	}
	if vaultPath != "" {
		finalVaultPath = vaultPath
	}

	var storage engine.Storage
	ext := filepath.Ext(finalVaultPath)
	if ext == ".db" || ext == ".sqlite" {
		sqliteStorage, err := engine.NewSQLiteStorage(finalVaultPath, cfg.Server.EncryptionKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize sqlite storage at %q: %w", finalVaultPath, err)
		}
		storage = sqliteStorage
	} else {
		storage = engine.NewJSONLStorage(finalVaultPath, cfg.Server.EncryptionKey)
	}

	graph, _, err := storage.LoadGraph()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load graph from %q: %w", finalVaultPath, err)
	}

	mgr := engine.NewManager(graph, storage)
	mgr.SignatSteering = cfg.EnableSignatSteering()
	mgr.AmbientTelemetry = cfg.EnableAmbientTelemetry()
	if cfg.Server != nil && cfg.Server.Options != nil && cfg.Server.Options.NumCtx != nil {
		mgr.NumCtx = *cfg.Server.Options.NumCtx
	}
	return mgr, cfg, nil
}

func resolveDiagnosticNode(mgr *engine.Manager, nodeQuery string) (*engine.Node, error) {
	if nodeQuery == "" {
		if head, err := mgr.Storage.GetSessionHead("main"); err == nil && head != "" {
			if n, err := mgr.GetNode(head); err == nil {
				return n, nil
			}
		}
		// Find leaf with the most recent timestamp
		allNodes := mgr.Graph.GetAllNodes()
		if len(allNodes) == 0 {
			return nil, fmt.Errorf("conversation graph is empty")
		}
		var latest *engine.Node
		for _, n := range allNodes {
			if len(mgr.Graph.GetChildren(n.ID)) == 0 {
				if latest == nil || n.Timestamp.After(latest.Timestamp) {
					latest = n
				}
			}
		}
		if latest != nil {
			return latest, nil
		}
		return allNodes[len(allNodes)-1], nil
	}

	// 1. Direct ID lookup
	if node, err := mgr.GetNode(nodeQuery); err == nil {
		return node, nil
	}

	// 2. Short ID lookup
	if node, err := mgr.FindNodeByShortID(nodeQuery); err == nil {
		return node, nil
	}

	return nil, fmt.Errorf("node not found matching ID or short prefix %q", nodeQuery)
}

func normalizeFlagArgs(args []string) []string {
	var flags []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				if arg != "-json" && arg != "--json" && arg != "-vision" && arg != "--vision" {
					flags = append(flags, args[i+1])
					i++
				}
			}
		} else {
			positional = append(positional, arg)
		}
	}
	return append(flags, positional...)
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to custom vault file")
	fs.StringVar(vaultPath, "v", "", "Path to custom vault file (shorthand)")
	configPath := fs.String("config", "", "Path to custom config JSON")
	fs.StringVar(configPath, "c", "", "Path to custom config JSON (shorthand)")
	jsonOutput := fs.Bool("json", false, "Output node inspection as JSON")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: please inspect [node-id] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Inspect conversation node metadata, tool observations, lineage, and resonance scores.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	_ = fs.Parse(normalizeFlagArgs(args))
	nodeQuery := ""
	if fs.NArg() > 0 {
		nodeQuery = fs.Arg(0)
	}

	mgr, cfg, err := setupDiagnosticsManager(*vaultPath, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	targetNode, err := resolveDiagnosticNode(mgr, nodeQuery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	path, err := mgr.GetPath(targetNode.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving lineage: %v\n", err)
		os.Exit(1)
	}

	// Calculate total path metrics
	var totalChars int
	for _, n := range path {
		totalChars += len(n.Content) + len(n.Thought)
		for _, o := range n.Observations {
			totalChars += len(o.Result)
		}
	}
	limit := mgr.NumCtx
	if limit <= 0 {
		limit = 32768
	}
	estimatedTokens := int(float64(totalChars) / 3.8)
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}
	fillRatio := float64(estimatedTokens) / float64(limit)

	if *jsonOutput {
		type NodeInspectJSON struct {
			TargetNode      *engine.Node `json:"target_node"`
			EstimatedTokens int          `json:"estimated_tokens"`
			NumCtx          int          `json:"num_ctx"`
			FillRatio       float64      `json:"fill_ratio"`
			PathLength      int          `json:"path_length"`
		}
		data, _ := json.MarshalIndent(NodeInspectJSON{
			TargetNode:      targetNode,
			EstimatedTokens: estimatedTokens,
			NumCtx:          limit,
			FillRatio:       fillRatio,
			PathLength:      len(path),
		}, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Human-readable formatted inspect output
	shortID := targetNode.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	signat := ""
	if targetNode.Metadata != nil {
		signat = targetNode.Metadata["signat"]
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("🔍 Node Inspection: %s (%s) %s\n", targetNode.ID, shortID, signat)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  • Role:       %s\n", targetNode.Role)
	fmt.Printf("  • Parent:     %s\n", targetNode.ParentID)
	fmt.Printf("  • Timestamp:  %s (%s ago)\n", targetNode.Timestamp.Format(time.RFC3339), time.Since(targetNode.Timestamp).Round(time.Second))
	if targetNode.Internal {
		fmt.Printf("  • Internal:   true (low-fidelity scratch node)\n")
	}

	// Content & Thought Summary
	fmt.Printf("  • Content:    %d chars\n", len(targetNode.Content))
	if len(targetNode.Content) > 0 {
		preview := strings.ReplaceAll(targetNode.Content, "\n", " ")
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		fmt.Printf("                %q\n", preview)
	}

	if len(targetNode.Thought) > 0 {
		fmt.Printf("  • Thought:    %d chars\n", len(targetNode.Thought))
		preview := strings.ReplaceAll(targetNode.Thought, "\n", " ")
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		fmt.Printf("                %q\n", preview)
	}

	// Segments
	if targetNode.Metadata != nil && targetNode.Metadata["segments"] != "" {
		var segs []engine.AssistantSegment
		if err := json.Unmarshal([]byte(targetNode.Metadata["segments"]), &segs); err == nil {
			fmt.Printf("  • Segments:   %d generation segments\n", len(segs))
		}
	}

	// Tools & Observations Breakdown
	fmt.Printf("\n🛠️  Tool Calls (%d) & Observations (%d):\n", len(targetNode.ToolCalls), len(targetNode.Observations))
	if len(targetNode.ToolCalls) == 0 && len(targetNode.Observations) == 0 {
		fmt.Println("    (None)")
	} else {
		for i, tc := range targetNode.ToolCalls {
			argsPreview := string(tc.Function.Arguments)
			if len(argsPreview) > 60 {
				argsPreview = argsPreview[:60] + "..."
			}
			fmt.Printf("    [%d] %s(%s) [ID: %s]\n", i+1, tc.Function.Name, argsPreview, tc.ID)

			var matchedObs *engine.ToolObservation
			for _, obs := range targetNode.Observations {
				if obs.ToolCallID == tc.ID {
					matchedObs = &obs
					break
				}
			}

			if matchedObs != nil {
				res := matchedObs.Result
				firstLine := res
				if idx := strings.Index(res, "\n"); idx != -1 {
					firstLine = strings.TrimSpace(res[:idx])
				}
				if len(firstLine) > 80 {
					firstLine = firstLine[:80] + "..."
				}
				fmt.Printf("        ↳ Observation: %d bytes | Header: %q\n", len(res), firstLine)
			} else {
				fmt.Printf("        ↳ Observation: [MISSING / PENDING]\n")
			}
		}
	}

	// Lineage & Resonance Scorecard
	vaultDisplay := "unknown"
	if cfg.Server != nil && cfg.Server.VaultPath != "" {
		vaultDisplay = cfg.Server.VaultPath
	}
	fmt.Println("\n📈 Lineage & Context Resonance Scorecard:")
	fmt.Printf("    Vault Path:        %s\n", vaultDisplay)
	fmt.Printf("    Context Limit:     %d tokens (num_ctx)\n", limit)
	fmt.Printf("    Active Path Load:  %d tokens (fill ratio: %.1f%%)\n\n", estimatedTokens, fillRatio*100)

	fmt.Printf("    %-5s %-10s %-10s %-8s %-8s %-8s %s\n", "Dist", "ShortID", "Role", "Cost", "Tokens", "Score V", "Fidelity Tier")
	fmt.Println("    ───────────────────────────────────────────────────────────────────")

	for i, n := range path {
		distance := len(path) - 1 - i
		score := mgr.CalculateResonanceScore(n, distance, fillRatio, len(path))
		if distance == 0 {
			score = 999.9 // active leaf
		}

		cost := len(n.Content) + len(n.Thought)
		for _, o := range n.Observations {
			cost += len(o.Result)
		}
		tokens := int(float64(cost) / 3.8)

		tier := "Low (Compact)"
		if score > 5.0 {
			tier = "Full Fidelity"
		} else if score > 0.5 {
			tier = "Medium"
		}

		nShort := n.ID
		if len(nShort) > 8 {
			nShort = nShort[:8]
		}

		scoreStr := fmt.Sprintf("%.2f", score)
		if n.Role == engine.RoleSystem || score > 900.0 {
			scoreStr = "PINNED"
			tier = "Full Fidelity"
		}

		fmt.Printf("    %-5d %-10s %-10s %-8d %-8d %-8s %s\n", distance, nShort, n.Role, cost, tokens, scoreStr, tier)
	}
	fmt.Println()
}

func runContext(args []string) {
	fs := flag.NewFlagSet("context", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to custom vault file")
	fs.StringVar(vaultPath, "v", "", "Path to custom vault file (shorthand)")
	configPath := fs.String("config", "", "Path to custom config JSON")
	fs.StringVar(configPath, "c", "", "Path to custom config JSON (shorthand)")
	jsonOutput := fs.Bool("json", false, "Output reconstructed context array as raw JSON")
	vision := fs.Bool("vision", false, "Enable vision message encoding (default: from config)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: please context [node-id] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Reconstruct and view the exact LLM prompt messages sent to providers for a node.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	_ = fs.Parse(normalizeFlagArgs(args))
	nodeQuery := ""
	if fs.NArg() > 0 {
		nodeQuery = fs.Arg(0)
	}

	mgr, cfg, err := setupDiagnosticsManager(*vaultPath, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	targetNode, err := resolveDiagnosticNode(mgr, nodeQuery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	supportsVision := *vision || cfg.SupportsVision()
	messages, err := mgr.BuildLLMContext(targetNode.ID, supportsVision)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building LLM context: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(messages, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Compute overall statistics
	var totalChars int
	for _, m := range messages {
		totalChars += len(m.Content)
		for _, o := range m.Observations {
			totalChars += len(o.Result)
		}
	}
	limit := mgr.NumCtx
	if limit <= 0 {
		limit = 32768
	}
	estTokens := int(float64(totalChars) / 3.8)
	fillPct := (float64(estTokens) / float64(limit)) * 100.0

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("📜 Reconstructed LLM Context for Node: %s\n", targetNode.ID)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  • Total Messages:    %d\n", len(messages))
	fmt.Printf("  • Estimated Tokens:  %d / %d (%.1f%% of context budget)\n", estTokens, limit, fillPct)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for i, m := range messages {
		header := fmt.Sprintf("[%02d] Role: %-9s", i+1, strings.ToUpper(string(m.Role)))
		if m.ToolCallID != "" {
			header += fmt.Sprintf(" | ToolCallID: %s", m.ToolCallID)
		}
		if len(m.ToolCalls) > 0 {
			var names []string
			for _, tc := range m.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			header += fmt.Sprintf(" | ToolCalls: %s", strings.Join(names, ", "))
		}
		if len(m.Images) > 0 {
			header += fmt.Sprintf(" | Images: %d", len(m.Images))
		}
		fmt.Println(header)

		// Print content
		content := m.Content
		if len(content) > 0 {
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				fmt.Printf("     %s\n", line)
			}
		} else if len(m.ToolCalls) > 0 {
			fmt.Printf("     (Tool Call Request)\n")
		} else {
			fmt.Printf("     (Empty content)\n")
		}
		fmt.Println()
	}
}
