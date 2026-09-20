//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/storage"
)

// setupE2E prepares an isolated Manager, Provider, and Harness for canary testing.
func setupE2E(t *testing.T) (*engine.Manager, providers.Provider, *config.Config) {
	if os.Getenv("PLEASE_E2E") == "" && os.Getenv("PLEASE_LIVE_FIRE") == "" {
		t.Skip("Skipping E2E canary testing (set PLEASE_E2E=1 to run)")
	}

	// Change working directory to project root so tools using relative paths work
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("failed to change directory to project root: %v", err)
	}

	// Use isolated ephemeral database in TempDir
	dbPath := filepath.Join(t.TempDir(), "canary.db")
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

	// Load configuration: check e2e.json, then livefire.json, then defaults
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

	// Environment variable overrides
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

	return mgr, provider, activeCfg
}

// contextWithTimeout returns a testing context with standard timeout.
func e2eContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
