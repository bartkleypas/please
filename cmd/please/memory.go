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
	"github.com/bartkleypas/please/internal/storage"
	"github.com/charmbracelet/lipgloss"
)

var (
	memHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e0e7e3")).Background(lipgloss.Color("#1e3a2f")).Padding(0, 1)
	memKeyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ade80"))
	memScopeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa"))
	memCatStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15"))
	memDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
)

func setupMemoryStore(vaultPath, configPath string) (storage.MemoryStore, *config.Config, error) {
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
	if vaultPath != "" {
		finalVaultPath = vaultPath
	} else if cfg.Server != nil && cfg.Server.VaultPath != "" {
		finalVaultPath = cfg.Server.VaultPath
	}

	if finalVaultPath == "" {
		// ADR 016 Discovery Ladder:
		// 1. Workspace-local anchor (.please/vault.db)
		if wsPleaseDir, ok := config.GetWorkspacePleaseDir(); ok {
			candidate := filepath.Join(wsPleaseDir, "vault.db")
			if _, err := os.Stat(candidate); err == nil {
				finalVaultPath = candidate
			}
		}
		// 2. Global anchor (~/.please/vault.db)
		if finalVaultPath == "" {
			if globalDir, err := config.GetGlobalPleaseDir(); err == nil {
				candidate := filepath.Join(globalDir, "vault.db")
				if _, err := os.Stat(candidate); err == nil {
					finalVaultPath = candidate
				}
			}
		}
		// 3. Test fixtures and legacy fallbacks
		if finalVaultPath == "" {
			for _, candidate := range []string{"test_vault/e2e.db", "vault.db", "e2e.db", "test_vault/livefire.db", "livefire.db"} {
				if _, err := os.Stat(candidate); err == nil {
					finalVaultPath = candidate
					break
				}
			}
		}
	}

	if finalVaultPath == "" {
		return nil, nil, fmt.Errorf("persistent agent memory requires an SQLite vault (.db/.sqlite). Specify with -v <path> or configure server.vault_path")
	}

	ext := filepath.Ext(finalVaultPath)
	if ext != ".db" && ext != ".sqlite" {
		return nil, nil, fmt.Errorf("persistent agent memory requires an SQLite vault (.db/.sqlite), current vault is %q", finalVaultPath)
	}

	sqliteStorage, err := engine.NewSQLiteStorage(finalVaultPath, cfg.Server.EncryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize sqlite storage at %q: %w", finalVaultPath, err)
	}

	return sqliteStorage, cfg, nil
}

func runMemory(args []string) {
	if len(args) == 0 {
		runMemoryList(nil)
		return
	}

	switch args[0] {
	case "list", "ls":
		runMemoryList(args[1:])
	case "inspect", "show", "get":
		runMemoryInspect(args[1:])
	case "diagnose", "diag", "stats":
		runMemoryDiagnose(args[1:])
	case "prune", "clean":
		runMemoryPrune(args[1:])
	case "--help", "-h", "help":
		printMemoryUsage()
	default:
		// If the first argument doesn't look like a subcommand, treat as "inspect <key>" or "list"
		if !strings.HasPrefix(args[0], "-") {
			runMemoryInspect(args)
		} else {
			runMemoryList(args)
		}
	}
}

func printMemoryUsage() {
	fmt.Fprintf(os.Stderr, "🧠 Please Memory: Cybernetic Persistent Agent Memory Diagnostics (ADR 014)\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  please memory [command] [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  list, ls                 List persistent memories matching filters (default)\n")
	fmt.Fprintf(os.Stderr, "  inspect <key>            Inspect full details and access telemetry of a memory\n")
	fmt.Fprintf(os.Stderr, "  diagnose, stats          Display health, category volume, and storage telemetry\n")
	fmt.Fprintf(os.Stderr, "  prune                    Clean stale or expired scratchpad memories\n\n")
	fmt.Fprintf(os.Stderr, "Common Options:\n")
	fmt.Fprintf(os.Stderr, "  -v, --vault <path>       Path to custom SQLite vault (.db)\n")
	fmt.Fprintf(os.Stderr, "  -c, --config <path>      Path to custom configuration JSON\n")
	fmt.Fprintf(os.Stderr, "      --scope <scope>      Filter scope: 'workspace', 'global', or 'session'\n")
	fmt.Fprintf(os.Stderr, "      --category <cat>     Filter category: 'preference', 'fact', 'architecture', 'constraint', 'workflow', 'scratchpad'\n")
	fmt.Fprintf(os.Stderr, "      --session <id>       Filter by session ID\n")
	fmt.Fprintf(os.Stderr, "  -q, --query <search>     Full-text search (FTS5) across keys and content\n")
	fmt.Fprintf(os.Stderr, "      --json               Output results as structured JSON\n")
}

func runMemoryList(args []string) {
	fs := flag.NewFlagSet("memory list", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to custom SQLite vault file")
	fs.StringVar(vaultPath, "v", "", "Path to custom SQLite vault file (shorthand)")
	configPath := fs.String("config", "", "Path to custom config JSON")
	fs.StringVar(configPath, "c", "", "Path to custom config JSON (shorthand)")
	scopeFlag := fs.String("scope", "", "Filter scope ('workspace', 'global', 'session')")
	catFlag := fs.String("category", "", "Filter category ('preference', 'fact', 'architecture', 'constraint', 'workflow', 'scratchpad')")
	sessionFlag := fs.String("session", "", "Filter session ID")
	queryFlag := fs.String("query", "", "Full-text search query")
	fs.StringVar(queryFlag, "q", "", "Full-text search query (shorthand)")
	limitFlag := fs.Int("limit", 50, "Maximum number of memories to return")
	jsonOutput := fs.Bool("json", false, "Output memories as JSON")

	_ = fs.Parse(normalizeFlagArgs(args))

	store, _, err := setupMemoryStore(*vaultPath, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	filter := storage.MemoryFilter{
		Scope:     storage.MemoryScope(*scopeFlag),
		Category:  storage.MemoryCategory(*catFlag),
		SessionID: *sessionFlag,
		Query:     *queryFlag,
		Limit:     *limitFlag,
	}

	mems, err := store.QueryMemories(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying memories: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(mems, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(mems) == 0 {
		fmt.Println("No memories found matching the specified filters.")
		return
	}

	maxKeyLen := 20
	for _, m := range mems {
		if len(m.Key) > maxKeyLen {
			maxKeyLen = len(m.Key)
		}
	}
	if maxKeyLen > 36 {
		maxKeyLen = 36
	}

	dividerLen := maxKeyLen + 85
	border := strings.Repeat("━", dividerLen)
	divider := strings.Repeat("─", dividerLen)

	fmt.Println(border)
	fmt.Println(memHeaderStyle.Render("🧠 Persistent Agent Memories (ADR 014)"))
	fmt.Println(border)

	colFmt := fmt.Sprintf("%%-%ds  %%-11s  %%-14s  %%-6s  %%-10s  %%s\n", maxKeyLen)
	fmt.Printf(colFmt, "KEY", "SCOPE", "CATEGORY", "ACCESS", "UPDATED", "CONTENT")
	fmt.Println(divider)

	for _, m := range mems {
		keyDisp := m.Key
		if len(keyDisp) > maxKeyLen {
			keyDisp = keyDisp[:maxKeyLen-2] + ".."
		}

		age := time.Since(m.UpdatedAt).Round(time.Minute).String()
		if age == "0s" {
			age = "just now"
		}

		contentClean := strings.ReplaceAll(m.Content, "\n", " ")
		if len(contentClean) > 40 {
			contentClean = contentClean[:37] + "..."
		}

		keyPadded := fmt.Sprintf(fmt.Sprintf("%%-%ds", maxKeyLen), keyDisp)
		scopePadded := fmt.Sprintf("%-11s", string(m.Scope))
		catPadded := fmt.Sprintf("%-14s", string(m.Category))
		accessPadded := fmt.Sprintf("%-6d", m.AccessCount)
		agePadded := fmt.Sprintf("%-10s", age)

		fmt.Printf("%s  %s  %s  %s  %s  %s\n",
			memKeyStyle.Render(keyPadded),
			memScopeStyle.Render(scopePadded),
			memCatStyle.Render(catPadded),
			accessPadded,
			memDimStyle.Render(agePadded),
			contentClean,
		)
	}
	fmt.Println(divider)
	fmt.Printf("Total: %d memories listed\n", len(mems))
}

func runMemoryInspect(args []string) {
	fs := flag.NewFlagSet("memory inspect", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to custom SQLite vault file")
	fs.StringVar(vaultPath, "v", "", "Path to custom SQLite vault file (shorthand)")
	configPath := fs.String("config", "", "Path to custom config JSON")
	fs.StringVar(configPath, "c", "", "Path to custom config JSON (shorthand)")
	scopeFlag := fs.String("scope", string(storage.ScopeWorkspace), "Memory scope ('workspace', 'global', 'session')")
	sessionFlag := fs.String("session", "", "Session ID (for session-scoped memories)")
	jsonOutput := fs.Bool("json", false, "Output inspection as JSON")

	_ = fs.Parse(normalizeFlagArgs(args))

	if fs.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "Usage: please memory inspect <key> [options]\n")
		os.Exit(1)
	}
	key := fs.Arg(0)

	store, _, err := setupMemoryStore(*vaultPath, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	scope := storage.MemoryScope(*scopeFlag)
	mem, err := store.GetMemory(scope, *sessionFlag, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving memory: %v\n", err)
		os.Exit(1)
	}
	if mem == nil {
		// Fallback 1: try ScopeGlobal if workspace failed
		if scope == storage.ScopeWorkspace {
			mem, _ = store.GetMemory(storage.ScopeGlobal, "", key)
		}
	}

	// Fallback 2: fuzzy / prefix search across keys in scope
	if mem == nil {
		candidates, _ := store.QueryMemories(storage.MemoryFilter{
			Scope:     scope,
			SessionID: *sessionFlag,
			Limit:     100,
		})
		lowerKey := strings.ToLower(key)
		var matches []storage.Memory
		for _, c := range candidates {
			if strings.Contains(strings.ToLower(c.Key), lowerKey) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 1 {
			mem = &matches[0]
		} else if len(matches) > 1 {
			fmt.Fprintf(os.Stderr, "Multiple memories match %q in scope %q:\n", key, scope)
			for _, m := range matches {
				fmt.Fprintf(os.Stderr, "  • %s (%s, %s)\n", memKeyStyle.Render(m.Key), m.Scope, m.Category)
			}
			os.Exit(1)
		}
	}

	// Fallback 3: fuzzy search in global scope if workspace query didn't match
	if mem == nil && scope == storage.ScopeWorkspace {
		globalCandidates, _ := store.QueryMemories(storage.MemoryFilter{
			Scope: storage.ScopeGlobal,
			Limit: 100,
		})
		lowerKey := strings.ToLower(key)
		var matches []storage.Memory
		for _, c := range globalCandidates {
			if strings.Contains(strings.ToLower(c.Key), lowerKey) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 1 {
			mem = &matches[0]
		} else if len(matches) > 1 {
			fmt.Fprintf(os.Stderr, "Multiple global memories match %q:\n", key)
			for _, m := range matches {
				fmt.Fprintf(os.Stderr, "  • %s (%s, %s)\n", memKeyStyle.Render(m.Key), m.Scope, m.Category)
			}
			os.Exit(1)
		}
	}

	if mem == nil {
		fmt.Fprintf(os.Stderr, "Error: memory %q not found in scope %q\n", key, scope)
		os.Exit(1)
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(mem, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("🧠 Memory Inspection: %s\n", memKeyStyle.Render(mem.Key))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  • Scope:          %s\n", memScopeStyle.Render(string(mem.Scope)))
	fmt.Printf("  • Category:       %s\n", memCatStyle.Render(string(mem.Category)))
	fmt.Printf("  • Confidence:     %.2f\n", mem.Confidence)
	if mem.SessionID != "" {
		fmt.Printf("  • Session ID:     %s\n", mem.SessionID)
	}
	if mem.SourceNodeID != "" {
		fmt.Printf("  • Source Node:    %s\n", mem.SourceNodeID)
	}
	if len(mem.Tags) > 0 {
		fmt.Printf("  • Tags:           %s\n", strings.Join(mem.Tags, ", "))
	}
	fmt.Printf("  • Access Count:   %d\n", mem.AccessCount)
	if mem.LastAccessedAt != nil {
		fmt.Printf("  • Last Accessed:  %s (%s ago)\n", mem.LastAccessedAt.Format(time.RFC3339), time.Since(*mem.LastAccessedAt).Round(time.Second))
	}
	fmt.Printf("  • Created:        %s\n", mem.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  • Updated:        %s (%s ago)\n", mem.UpdatedAt.Format(time.RFC3339), time.Since(mem.UpdatedAt).Round(time.Second))

	if len(mem.Metadata) > 0 {
		metaJSON, _ := json.Marshal(mem.Metadata)
		fmt.Printf("  • Metadata:       %s\n", string(metaJSON))
	}

	fmt.Println("────────────────────────────────────────────────────────────────────────────")
	fmt.Println("Content:")
	fmt.Println(mem.Content)
	fmt.Println("────────────────────────────────────────────────────────────────────────────")
}

func runMemoryDiagnose(args []string) {
	fs := flag.NewFlagSet("memory diagnose", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to custom SQLite vault file")
	fs.StringVar(vaultPath, "v", "", "Path to custom SQLite vault file (shorthand)")
	configPath := fs.String("config", "", "Path to custom config JSON")
	fs.StringVar(configPath, "c", "", "Path to custom config JSON (shorthand)")
	scopeFlag := fs.String("scope", "", "Filter scope ('workspace', 'global', 'session')")
	sessionFlag := fs.String("session", "", "Filter session ID")
	jsonOutput := fs.Bool("json", false, "Output diagnostics as JSON")

	_ = fs.Parse(normalizeFlagArgs(args))

	store, _, err := setupMemoryStore(*vaultPath, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	diag, err := store.DiagnoseMemories(storage.MemoryScope(*scopeFlag), *sessionFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving diagnostics: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(diag, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println(memHeaderStyle.Render("📊 Memory Vault Telemetry & Health"))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  • Total Memories:   %d\n", diag.TotalMemories)
	fmt.Printf("  • Storage Footprint: %.2f KB (%d bytes)\n", float64(diag.StorageBytes)/1024.0, diag.StorageBytes)

	fmt.Println("\n  Scope Distribution:")
	for sc, count := range diag.ByScope {
		fmt.Printf("    - %s: %d\n", memScopeStyle.Render(fmt.Sprintf("%-12s", string(sc))), count)
	}

	fmt.Println("\n  Category Distribution:")
	for cat, count := range diag.ByCategory {
		fmt.Printf("    - %s: %d\n", memCatStyle.Render(fmt.Sprintf("%-14s", string(cat))), count)
	}

	if len(diag.MostAccessed) > 0 {
		fmt.Println("\n  🔥 Most Accessed Memories:")
		for i, m := range diag.MostAccessed {
			if i >= 5 {
				break
			}
			fmt.Printf("    %d. %s (%s, %d accesses)\n", i+1, memKeyStyle.Render(m.Key), m.Scope, m.AccessCount)
		}
	}

	if len(diag.StaleCandidates) > 0 {
		fmt.Println("\n  💤 Stale Candidates (Unaccessed > 30 days):")
		for i, m := range diag.StaleCandidates {
			if i >= 5 {
				break
			}
			fmt.Printf("    %d. %s (%s, updated %s ago)\n", i+1, memKeyStyle.Render(m.Key), m.Scope, time.Since(m.UpdatedAt).Round(time.Hour*24))
		}
	}
	fmt.Println("────────────────────────────────────────────────────────────────────────────")
}

func runMemoryPrune(args []string) {
	fs := flag.NewFlagSet("memory prune", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to custom SQLite vault file")
	fs.StringVar(vaultPath, "v", "", "Path to custom SQLite vault file (shorthand)")
	configPath := fs.String("config", "", "Path to custom config JSON")
	fs.StringVar(configPath, "c", "", "Path to custom config JSON (shorthand)")
	olderThanFlag := fs.Duration("older-than", 0, "Prune memories unaccessed or updated older than duration (e.g. 720h for 30d)")
	categoryFlag := fs.String("category", string(storage.CategoryScratchpad), "Category to prune (default: scratchpad)")
	dryRun := fs.Bool("dry-run", false, "Simulate pruning without deleting records")
	jsonOutput := fs.Bool("json", false, "Output results as JSON")

	_ = fs.Parse(normalizeFlagArgs(args))

	store, _, err := setupMemoryStore(*vaultPath, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	mems, err := store.QueryMemories(storage.MemoryFilter{
		Category: storage.MemoryCategory(*categoryFlag),
		Limit:    500,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying memories for pruning: %v\n", err)
		os.Exit(1)
	}

	now := time.Now()
	var candidates []storage.Memory
	for _, m := range mems {
		if *olderThanFlag > 0 {
			cutoff := now.Add(-*olderThanFlag)
			if m.UpdatedAt.After(cutoff) {
				continue
			}
			if m.LastAccessedAt != nil && m.LastAccessedAt.After(cutoff) {
				continue
			}
		}
		candidates = append(candidates, m)
	}

	type PruneResult struct {
		CandidatesCount int      `json:"candidates_count"`
		DeletedCount    int      `json:"deleted_count"`
		DryRun          bool     `json:"dry_run"`
		PrunedKeys      []string `json:"pruned_keys"`
	}

	res := PruneResult{
		CandidatesCount: len(candidates),
		DryRun:          *dryRun,
	}

	for _, c := range candidates {
		res.PrunedKeys = append(res.PrunedKeys, c.Key)
		if !*dryRun {
			if err := store.DeleteMemory(c.Scope, c.SessionID, c.Key); err == nil {
				res.DeletedCount++
			}
		}
	}

	if *jsonOutput {
		data, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(data))
		return
	}

	if *dryRun {
		fmt.Printf("🔍 [Dry Run] %d memories would be pruned from category %q:\n", len(candidates), *categoryFlag)
	} else {
		fmt.Printf("🧹 Pruned %d/%d memories from category %q:\n", res.DeletedCount, len(candidates), *categoryFlag)
	}
	for _, k := range res.PrunedKeys {
		fmt.Printf("  - %s\n", memKeyStyle.Render(k))
	}
}
