//go:build e2e

package scenarios

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
)

func setupArchivist(t *testing.T) (*engine.Manager, providers.Provider, *config.Config, string) {
	if os.Getenv("PLEASE_E2E") == "" && os.Getenv("PLEASE_LIVE_FIRE") == "" {
		t.Skip("Skipping E2E scenario testing (set PLEASE_E2E=1 to run)")
	}

	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("failed to change directory to project root: %v", err)
	}

	testVaultDir := "test_vault"
	_ = os.MkdirAll(testVaultDir, 0755)
	dbPath := filepath.Join(testVaultDir, "e2e.db")

	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	store, err := storage.NewSQLiteStorage(dbPath, "")
	if err != nil {
		t.Fatalf("failed to initialize sqlite storage: %v", err)
	}

	g, _, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	mgr := engine.NewManager(g, store)
	mgr.RegisterDefaultTools(".")

	var activeCfg *config.Config
	if cfg, err := config.LoadConfigFile("e2e.json"); err == nil {
		activeCfg = cfg
	} else if cfg, err := config.LoadConfigFile("livefire.json"); err == nil {
		activeCfg = cfg
	} else if cfg, err := config.LoadConfig(); err == nil {
		activeCfg = cfg
	} else {
		activeCfg = config.NewDefaultConfig()
	}

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

	return mgr, provider, activeCfg, dbPath
}

// TestScenario_GeorgeArchivist executes the canonical 5-turn autonomous chronicle
// and synthesizes an authoritative milestone Supernode.
func TestScenario_GeorgeArchivist(t *testing.T) {
	mgr, provider, cfg, dbPath := setupArchivist(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// 1. Genesis Root Node: George the Archivist Persona
	sysPrompt := "You are George the Archivist 🦉📚, an 18-inch tall barred owl who chronicles software architecture. You speak with a sardonic wit and deep appreciation for structured DAGs."
	sysNode, err := mgr.CreateNode("", graph.RoleSystem, sysPrompt, false)
	if err != nil {
		t.Fatalf("failed to create system node: %v", err)
	}

	harness := engine.NewSessionHarness(mgr, provider, cfg)

	// 2. Mission Primer Turn
	primerPrompt := "Greetings George! Over the next 5 turns, explore this workspace autonomously. On each turn, use the read_file tool to inspect a different core file (e.g. README.md, docs/context_resonance.md, GEMINI.md, internal/engine/service.go, internal/tui/map.go) and weave your findings into the lore of Please. Your continuation prompt will always be simply: 'Please proceed.'"
	primerTurn, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
		ParentID: sysNode.ID,
		Message:  primerPrompt,
		Role:     string(graph.RoleUser),
	}, nil)
	if err != nil {
		t.Fatalf("primer turn failed: %v", err)
	}

	var turnIDs []string
	turnIDs = append(turnIDs, primerTurn.ParentID, primerTurn.ID)
	currParentID := primerTurn.ID

	// 3. 4 autonomous follow-up turns fueled only by "Please proceed."
	for i := 2; i <= 5; i++ {
		t.Logf("=== Starting Autonomous Vector Turn %d/5 ===", i)
		asstTurn, err := harness.ExecuteTurn(ctx, engine.TurnRequest{
			ParentID: currParentID,
			Message:  "Please proceed.",
			Role:     string(graph.RoleUser),
		}, nil)
		if err != nil {
			t.Fatalf("turn %d failed: %v", i, err)
		}

		turnIDs = append(turnIDs, asstTurn.ParentID, asstTurn.ID)
		currParentID = asstTurn.ID
		t.Logf("Turn %d completed (Node: %s, Signat: %q)", i, asstTurn.ID, asstTurn.Metadata["signat"])
	}

	// 4. Synthesize 5-Turn Milestone Compaction into a Supernode
	t.Log("=== Compacting Autonomous Vector into Supernode ===")
	superNode, err := mgr.CompactRangeWithDirective(
		ctx,
		provider,
		turnIDs,
		"synthesize George's 5-step workspace chronicle into an authoritative architectural milestone",
	)
	if err != nil {
		t.Fatalf("failed to compact autonomous vector range: %v", err)
	}

	t.Logf("### Generated Supernode (ID: %s):\n%s", superNode.ID, superNode.Content)

	if superNode.Role != graph.RoleSummary {
		t.Errorf("expected supernode to have RoleSummary, got %s", superNode.Role)
	}
	if superNode.ParentID != sysNode.ID {
		t.Errorf("expected supernode ParentID to be system root (%s), got: %s", sysNode.ID, superNode.ParentID)
	}
	if !strings.Contains(superNode.Content, "🎯 Trajectory:") {
		t.Errorf("expected supernode to contain '🎯 Trajectory:' header, got:\n%s", superNode.Content)
	}

	t.Logf("✓ George the Archivist scenario complete! Database seeded at: %s", dbPath)
}
