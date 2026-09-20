package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
)

func main() {
	vaultPath := flag.String("vault", "test_vault/e2e.db", "Destination path for seeded SQLite vault")
	configPath := flag.String("config", "e2e.json", "Path to config file (e2e.json or livefire.json)")
	turns := flag.Int("turns", 5, "Number of autonomous chronicle turns")
	flag.Parse()

	absVault, _ := filepath.Abs(*vaultPath)
	fmt.Printf("🦉 Seeding living showcase database at: %s\n", absVault)

	// 1. Initialize isolated vault
	_ = os.MkdirAll(filepath.Dir(absVault), 0755)
	_ = os.Remove(absVault)
	_ = os.Remove(absVault + "-wal")
	_ = os.Remove(absVault + "-shm")

	store, err := storage.NewSQLiteStorage(absVault, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating vault: %v\n", err)
		os.Exit(1)
	}

	// 2. Load configuration
	var activeCfg *config.Config
	if cfg, err := config.LoadConfigFile(*configPath); err == nil {
		activeCfg = cfg
	} else if cfg, err := config.LoadConfigFile("livefire.json"); err == nil {
		activeCfg = cfg
	} else if cfg, err := config.LoadConfig(); err == nil {
		activeCfg = cfg
	} else {
		activeCfg = config.NewDefaultConfig()
	}

	g, _, err := store.LoadGraph()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading graph: %v\n", err)
		os.Exit(1)
	}

	mgr := engine.NewManager(g, store)
	mgr.RegisterDefaultTools(".")

	providerType := "ollama"
	endpoint := "http://localhost:11434/api/chat"
	model := "gemma4:e4b"
	apiKey := ""
	var options *providers.ModelOptions

	if activeCfg.Server != nil {
		mgr.SignatSteering = activeCfg.EnableSignatSteering()
		mgr.AmbientTelemetry = activeCfg.EnableAmbientTelemetry()
		if activeCfg.Server.Provider != "" {
			providerType = activeCfg.Server.Provider
		}
		if activeCfg.Server.Endpoint != "" {
			endpoint = activeCfg.Server.Endpoint
		}
		if activeCfg.Server.Model != "" {
			model = activeCfg.Server.Model
		}
		if activeCfg.Server.APIKey != "" {
			apiKey = activeCfg.Server.APIKey
		}
		if activeCfg.Server.Options != nil {
			options = activeCfg.Server.Options
		}
	}

	if envProvider := os.Getenv("PLEASE_PROVIDER"); envProvider != "" {
		providerType = envProvider
	}
	if envModel := os.Getenv("OLLAMA_MODEL"); envModel != "" {
		model = envModel
	}
	if envEndpoint := os.Getenv("OLLAMA_ENDPOINT"); envEndpoint != "" {
		endpoint = envEndpoint
	}
	if envAPIKey := os.Getenv("OPENAI_API_KEY"); envAPIKey != "" {
		apiKey = envAPIKey
	}

	var provider providers.Provider
	if providerType == "openai" {
		provider = providers.NewOpenAIProvider(endpoint, model, apiKey, options)
	} else {
		provider = providers.NewOllamaProvider(endpoint, model, options)
	}

	harness := engine.NewSessionHarness(mgr, provider, activeCfg)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// 3. Genesis Node 0: George the Archivist
	sysPrompt := "You are George the Archivist 🦉📚, an 18-inch tall barred owl who chronicles software architecture. You speak with a sardonic wit and deep appreciation for structured DAGs."
	sysNode, err := mgr.CreateNode("", graph.RoleSystem, sysPrompt, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating genesis node: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  • Genesis Root Node established (ID: %s)\n", sysNode.ID)

	// 4. Mission Primer Turn
	primerPrompt := fmt.Sprintf("Greetings George! Over the next %d turns, explore this workspace autonomously. On each turn, use the read_file tool to inspect a different core file (e.g. README.md, docs/context_resonance.md, GEMINI.md, internal/engine/service.go, internal/tui/map.go) and weave your findings into the lore of Please. Your continuation prompt will always be simply: 'Please proceed.'", *turns)
	fmt.Println("  • Dispatching Mission Primer Turn...")

	primerTurn, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		ParentID: sysNode.ID,
		Message:  primerPrompt,
		Role:     string(graph.RoleUser),
	}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing primer turn: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("    ✓ Primer turn complete (Node: %s, Signat: %q)\n", primerTurn.ID, primerTurn.Metadata["signat"])

	var turnIDs []string
	turnIDs = append(turnIDs, primerTurn.ParentID, primerTurn.ID)
	currParentID := primerTurn.ID

	// 5. Autonomous Follow-Up Turns
	for i := 2; i <= *turns; i++ {
		fmt.Printf("  • Running Autonomous Chronicle Turn %d/%d...\n", i, *turns)
		asstTurn, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
			ParentID: currParentID,
			Message:  "Please proceed.",
			Role:     string(graph.RoleUser),
		}, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error executing turn %d: %v\n", i, err)
			os.Exit(1)
		}
		turnIDs = append(turnIDs, asstTurn.ParentID, asstTurn.ID)
		currParentID = asstTurn.ID
		fmt.Printf("    ✓ Turn %d complete (Node: %s, Signat: %q)\n", i, asstTurn.ID, asstTurn.Metadata["signat"])
	}

	// 6. Synthesize Milestone Compaction Supernode
	fmt.Println("  • Synthesizing Milestone Supernode Compaction...")
	superNode, err := mgr.CompactRangeWithDirective(
		ctx,
		provider,
		turnIDs,
		fmt.Sprintf("synthesize George's %d-step workspace chronicle into an authoritative architectural milestone", *turns),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compacting autonomous vector: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("    ✓ Supernode created (ID: %s, Role: %s)\n", superNode.ID, superNode.Role)

	// Summary output
	fmt.Println(strings.Repeat("━", 75))
	fmt.Println("🎉 Living showcase database successfully seeded!")
	fmt.Printf("   • Vault Path: %s\n", *vaultPath)
	fmt.Printf("   • Total Nodes: %d (1 Root, %d User/Assistant turns, 1 Supernode)\n", len(mgr.Graph.Nodes), *turns*2)
	fmt.Printf("   • Active Head: %s\n", superNode.ID)
	fmt.Println(strings.Repeat("─", 75))
	fmt.Printf("To explore the seeded narrative in the TUI, run:\n")
	fmt.Printf("   please -v %s\n", *vaultPath)
	fmt.Println(strings.Repeat("━", 75))
}
