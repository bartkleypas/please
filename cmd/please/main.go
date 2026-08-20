package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/server"
	"github.com/bartkleypas/please/internal/tui"

	tea "github.com/charmbracelet/bubbletea"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join(*i, ",")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	// Define flags
	chatFlag := flag.Bool("chat", false, "Start the TUI chat interface")
	flag.BoolVar(chatFlag, "c", false, "Start the TUI chat interface (shorthand)")

	vaultPath := flag.String("vault", "", "Path to a custom vault.jsonl or .db file")
	flag.StringVar(vaultPath, "v", "", "Path to a custom vault file (shorthand)")

	parent := flag.String("parent", "", "Parent node ID for new message")
	flag.StringVar(parent, "p", "", "Parent node ID (shorthand)")

	jumpID := flag.String("jump", "", "Node ID to jump to in interactive mode")
	flag.StringVar(jumpID, "j", "", "Node ID to jump to in interactive mode (shorthand)")

	serverPort := flag.Int("server", 0, "Start visualization server on specified port")
	flag.IntVar(serverPort, "s", 0, "Start server on port (shorthand)")

	noGen := flag.Bool("no-gen", false, "Disable automatic LLM generation when passing a message")
	roleStr := flag.String("role", "", "Override role for the new node (user, assistant, system, tool)")
	versionFlag := flag.Bool("version", false, "Print the application version and exit")

	tempFlag := flag.Float64("temperature", -1.0, "Sampling temperature (e.g., 0.7)")
	flag.Float64Var(tempFlag, "t", -1.0, "Sampling temperature (shorthand)")
	topPFlag := flag.Float64("top-p", -1.0, "Top-p sampling parameter (e.g., 0.9)")
	topKFlag := flag.Int("top-k", -1, "Top-k sampling parameter (e.g., 40)")
	ctxFlag := flag.Int("num-ctx", 0, "Context window size in tokens (e.g., 16384)")
	flag.IntVar(ctxFlag, "ctx", 0, "Context window size in tokens (shorthand)")
	maxTokensFlag := flag.Int("max-tokens", 0, "Maximum response tokens to generate (e.g., 2048)")

	var images arrayFlags
	flag.Var(&images, "image", "Path to an image to attach (can be specified multiple times)")
	flag.Var(&images, "i", "Path to an image to attach (shorthand)")
	infoFlag := flag.Bool("info", false, "Print image metadata info and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🦉 Please: A DAG-based TUI for branching LLM conversations.\n\n")
		fmt.Fprintf(os.Stderr, "Usage: please [options] [message...]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -c, --chat             Start the TUI chat interface\n")
		fmt.Fprintf(os.Stderr, "  -v, --vault <path>     Path to a custom vault file\n")
		fmt.Fprintf(os.Stderr, "  -p, --parent <id>      Parent node ID for new message\n")
		fmt.Fprintf(os.Stderr, "  -j, --jump <id>        Node ID to jump to in interactive mode\n")
		fmt.Fprintf(os.Stderr, "  -s, --server <port>    Start visualization server on port\n")
		fmt.Fprintf(os.Stderr, "  -t, --temperature <f>  Sampling temperature (0.0 - 2.0)\n")
		fmt.Fprintf(os.Stderr, "      --top-p <f>        Top-p nucleus sampling (0.0 - 1.0)\n")
		fmt.Fprintf(os.Stderr, "      --top-k <i>        Top-k sampling\n")
		fmt.Fprintf(os.Stderr, "      --ctx, --num-ctx   Context window size in tokens\n")
		fmt.Fprintf(os.Stderr, "      --max-tokens <i>   Maximum generation tokens\n")
		fmt.Fprintf(os.Stderr, "      --no-gen           Disable auto-generation when passing a message\n")
		fmt.Fprintf(os.Stderr, "      --role <role>      Override role (user, tool, system, assistant)\n")
		fmt.Fprintf(os.Stderr, "\nCommands inside the TUI:\n")
		fmt.Fprintf(os.Stderr, "  /help    Show internal command list\n")
		fmt.Fprintf(os.Stderr, "  /map     Visualize conversation branches\n")
		fmt.Fprintf(os.Stderr, "  /config  View or adjust model and runner settings\n")
		fmt.Fprintf(os.Stderr, "  /server  Control the web visualization server\n")
	}

	flag.Parse()

	if *versionFlag {
		fmt.Printf("please version %s\n", engine.Version)
		os.Exit(0)
	}

	if *infoFlag && len(images) > 0 {
		for _, img := range images {
			file, err := os.Open(img)
			if err != nil {
				fmt.Printf("Error opening %s: %v\n", img, err)
				continue
			}
			cfg, format, err := image.DecodeConfig(file)
			if err != nil {
				fmt.Printf("Error decoding %s: %v\n", img, err)
				file.Close()
				continue
			}
			fmt.Printf("File: %s\n", img)
			fmt.Printf("Format: %s\n", format)
			fmt.Printf("Dimensions: %dx%d\n", cfg.Width, cfg.Height)
			if strings.ToLower(format) == "png" {
				_, _ = file.Seek(0, 0)
				meta, err := engine.ExtractPNGMetadata(file)
				if err == nil {
					if params, exists := meta["parameters"]; exists {
						fmt.Printf("Stable Diffusion Parameters:\n%s\n", params)
					}
				}
			}
			file.Close()
			fmt.Println()
		}
		os.Exit(0)
	}

	// Load Configuration
	cfg, err := engine.LoadConfig()
	if err != nil {
		fmt.Printf("Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI flag overrides to cfg.Options
	if *tempFlag >= 0 || *topPFlag >= 0 || *topKFlag >= 0 || *ctxFlag > 0 || *maxTokensFlag > 0 {
		if cfg.Options == nil {
			cfg.Options = &engine.ModelOptions{}
		}
		if *tempFlag >= 0 {
			cfg.Options.Temperature = tempFlag
		}
		if *topPFlag >= 0 {
			cfg.Options.TopP = topPFlag
		}
		if *topKFlag >= 0 {
			cfg.Options.TopK = topKFlag
		}
		if *ctxFlag > 0 {
			cfg.Options.NumCtx = ctxFlag
		}
		if *maxTokensFlag > 0 {
			cfg.Options.MaxTokens = maxTokensFlag
		}
	}

	// Initialize Storage
	finalVaultPath := cfg.VaultPath
	if *vaultPath != "" {
		finalVaultPath = *vaultPath
	}

	storageType := cfg.StorageType
	if strings.HasSuffix(finalVaultPath, ".db") {
		storageType = "sqlite"
	} else if strings.HasSuffix(finalVaultPath, ".jsonl") {
		storageType = "jsonl"
	}

	var storage engine.Storage
	if storageType == "sqlite" {
		storage, err = engine.NewSQLiteStorage(finalVaultPath, cfg.EncryptionKey)
		if err != nil {
			fmt.Printf("Error initializing sqlite storage: %v\n", err)
			os.Exit(1)
		}
	} else {
		storage = engine.NewJSONLStorage(finalVaultPath, cfg.EncryptionKey)
	}

	graph, lastID, err := storage.LoadGraph()
	if err != nil {
		fmt.Printf("Error loading graph: %v\n", err)
		os.Exit(1)
	}

	var provider engine.LLMProvider
	if cfg.Provider == "openai" {
		provider = engine.NewOpenAIProvider(cfg.Endpoint, cfg.Model, cfg.APIKey, cfg.Options)
	} else {
		provider = engine.NewOllamaProvider(cfg.Endpoint, cfg.Model, cfg.Options)
	}
	mgr := engine.NewManager(graph, storage)
	webServer := server.NewServer(mgr)

	// Determine if we have a message from args or stdin
	var pipedContent string
	var pipedRole engine.Role = engine.RoleTool

	stat, statErr := os.Stdin.Stat()
	if statErr == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		// Data is being piped in
		bytes, err := io.ReadAll(os.Stdin)
		if err == nil {
			pipedContent = strings.TrimSpace(string(bytes))
		}
	}

	var argContent string
	if flag.NArg() > 0 {
		argContent = strings.Join(flag.Args(), " ")
	}

	// If explicit role is provided, override the piped input role
	if *roleStr != "" {
		pipedRole = engine.Role(*roleStr)
	}

	// Handle Message Mode (either piped input, args, or both)
	if pipedContent != "" || argContent != "" {
		parentID := *parent
		if parentID == "" {
			if pipedContent != "" && pipedRole == engine.RoleSystem {
				// The Silicon Seed: Create a new narrative root for system prompts
				parentID = ""
			} else {
				parentID = lastID
			}
		}

		var finalID string

		// 1. If we have piped content, create a node for it first
		if pipedContent != "" {
			newNode, err := mgr.CreateNode(parentID, pipedRole, pipedContent, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating piped node: %v\n", err)
				os.Exit(1)
			}
			finalID = newNode.ID
			parentID = finalID // For the subsequent argument node, the parent is the piped node
		}

		// 2. If we also have arguments, create a user node parented by the piped node
		if argContent != "" {
			newNode, err := mgr.CreateNode(parentID, engine.RoleUser, argContent, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating message node: %v\n", err)
				os.Exit(1)
			}
			finalID = newNode.ID
		}

		if len(images) > 0 {
			finalNode, err := mgr.GetNode(finalID)
			if err == nil {
				mgr.AttachImages(finalNode, images)
				_ = mgr.Storage.SaveNode(finalNode)
			}
		}

		if !*noGen {
			messages, err := mgr.BuildLLMContext(finalID, cfg.SupportsVision())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error building context for generation: %v\n", err)
				os.Exit(1)
			}

			resp, err := provider.GenerateResponse(context.Background(), messages, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error generating response: %v\n", err)
				os.Exit(1)
			}

			assistantNode, err := mgr.CreateAssistantNode(finalID, resp.Content, resp.Thought, resp.ToolCalls, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error saving assistant node: %v\n", err)
				os.Exit(1)
			}
			finalID = assistantNode.ID
		}

		fmt.Println(finalID)
		os.Exit(0)
	}

	// Handle Server
	if *serverPort > 0 {
		if err := webServer.Start(*serverPort); err != nil {
			fmt.Printf("Error starting web server: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Web server started on http://localhost:%d\n", *serverPort)
	}

	// Start TUI
	startID := lastID
	if *jumpID != "" {
		startID = *jumpID
	}

	m := tui.NewModel(cfg, graph, storage, provider, startID)
	m.Server = webServer
	p := tea.NewProgram(&m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there was an error running the TUI: %v", err)
		os.Exit(1)
	}
}
