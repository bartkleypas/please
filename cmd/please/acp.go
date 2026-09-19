package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bartkleypas/please/internal/acp"
	"github.com/bartkleypas/please/internal/engine"
)

func runACP(args []string) {
	fs := flag.NewFlagSet("acp", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to a custom configuration JSON file")
	fs.StringVar(configPath, "c", "", "Path to a custom configuration JSON file (shorthand)")

	vaultPath := fs.String("vault", "", "Path to a custom vault.jsonl or .db file")
	fs.StringVar(vaultPath, "v", "", "Path to a custom vault file (shorthand)")

	workspacePath := fs.String("workspace", "", "Path to the project workspace directory")
	fs.StringVar(workspacePath, "w", "", "Path to the project workspace directory (shorthand)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: please acp [options]\n\n")
		fmt.Fprintf(os.Stderr, "Starts the Agent Client Protocol (ACP) stdio JSON-RPC server for IDE integration (Zed, JetBrains, Xcode).\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Load configuration
	var cfg *engine.Config
	var err error
	if *configPath != "" {
		cfg, err = engine.LoadConfigFile(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg, err = engine.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading default config: %v\n", err)
			os.Exit(1)
		}
	}

	if cfg.Server == nil {
		cfg.Server = &engine.ServerConfig{}
	}

	if *workspacePath != "" {
		cfg.Server.WorkspaceDir = *workspacePath
	}

	finalVaultPath := cfg.Server.VaultPath
	if *vaultPath != "" {
		finalVaultPath = *vaultPath
	}

	storageType := cfg.Server.StorageType
	if strings.HasSuffix(finalVaultPath, ".db") {
		storageType = "sqlite"
	} else if strings.HasSuffix(finalVaultPath, ".jsonl") {
		storageType = "jsonl"
	}

	var storage engine.Storage
	if storageType == "sqlite" {
		storage, err = engine.NewSQLiteStorage(finalVaultPath, cfg.Server.EncryptionKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing sqlite storage: %v\n", err)
			os.Exit(1)
		}
	} else {
		storage = engine.NewJSONLStorage(finalVaultPath, cfg.Server.EncryptionKey)
	}

	graph, _, err := storage.LoadGraph()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading graph: %v\n", err)
		os.Exit(1)
	}

	var provider engine.LLMProvider
	if cfg.Server.Provider == "openai" {
		provider = engine.NewOpenAIProvider(cfg.Server.Endpoint, cfg.Server.Model, cfg.Server.APIKey, cfg.Server.Options)
	} else {
		provider = engine.NewOllamaProvider(cfg.Server.Endpoint, cfg.Server.Model, cfg.Server.Options)
	}

	mgr := engine.NewManager(graph, storage)
	mgr.SignatSteering = cfg.EnableSignatSteering()
	mgr.AmbientTelemetry = cfg.EnableAmbientTelemetry()
	mgr.RegisterDefaultTools(cfg.GetWorkspaceDir())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := acp.Run(ctx, mgr, provider, cfg, os.Stdin, os.Stdout); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "ACP server exited with error: %v\n", err)
		os.Exit(1)
	}
}
