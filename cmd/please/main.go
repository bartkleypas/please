package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"org.kleypas.please/internal/engine"
	"org.kleypas.please/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Define flags
	chatFlag := flag.Bool("chat", false, "Start the TUI chat interface")
	flag.BoolVar(chatFlag, "c", false, "Start the TUI chat interface (shorthand)")

	vaultPath := flag.String("vault", "", "Path to a custom vault.jsonl file")
	flag.StringVar(vaultPath, "v", "", "Path to a custom vault.jsonl file (shorthand)")

	pipeMode := flag.Bool("pipe", false, "Run in non-interactive pipe mode")
	flag.BoolVar(pipeMode, "p", false, "Run in non-interactive pipe mode (shorthand)")

	message := flag.String("message", "", "Message content for pipe mode")
	flag.StringVar(message, "m", "", "Message content for pipe mode (shorthand)")

	parent := flag.String("parent", "", "Parent node ID for pipe mode")
	roleStr := flag.String("role", string(engine.RoleUser), "Role for the new node (user, assistant, system, tool)")
	jumpID := flag.String("jump", "", "Node ID to jump to in interactive mode")
	flag.StringVar(jumpID, "j", "", "Node ID to jump to in interactive mode (shorthand)")

	generate := flag.Bool("generate", false, "Trigger LLM generation in pipe mode")
	flag.BoolVar(generate, "g", false, "Trigger LLM generation in pipe mode (shorthand)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🦉 Please: A DAG-based TUI for branching LLM conversations.\n\n")
		fmt.Fprintf(os.Stderr, "Usage: please [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nCommands inside the TUI:\n")
		fmt.Fprintf(os.Stderr, "  /help    Show internal command list\n")
		fmt.Fprintf(os.Stderr, "  /map     Visualize conversation branches\n")
	}

	flag.Parse()

	if !*chatFlag && !*pipeMode {
		flag.Usage()
		os.Exit(0)
	}

	// Load Configuration
	cfg, err := engine.LoadConfig()
	if err != nil {
		fmt.Printf("Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Initialize Storage based on config or flag
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
		var err error
		storage, err = engine.NewSQLiteStorage(finalVaultPath)
		if err != nil {
			fmt.Printf("Error initializing sqlite storage: %v\n", err)
			os.Exit(1)
		}
	} else {
		storage = engine.NewJSONLStorage(finalVaultPath)
	}

	graph, lastID, err := storage.LoadGraph()
	if err != nil {
		fmt.Printf("Error loading graph: %v\n", err)
		os.Exit(1)
	}

	// Initialize LLM Provider using settings from config
	provider := engine.NewOllamaProvider(cfg.Endpoint, cfg.Model)
	mgr := engine.NewManager(graph, storage)

	if *pipeMode {
		content := *message
		if content == "" {
			// Read from Stdin if message flag is empty
			stdinContent, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
				os.Exit(1)
			}
			content = strings.TrimSpace(string(stdinContent))
		}

		if content == "" {
			fmt.Fprintf(os.Stderr, "Error: No message content provided via -m or stdin\n")
			os.Exit(1)
		}

		parentID := *parent
		if parentID == "" {
			parentID = lastID
		}

		// Validate role
		role := engine.Role(*roleStr)
		newNode, err := mgr.CreateNode(parentID, role, content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating node: %v\n", err)
			os.Exit(1)
		}

		finalID := newNode.ID

		if *generate {
			path, err := mgr.GetPath(finalID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting path for generation: %v\n", err)
				os.Exit(1)
			}

			var messages []engine.Message
			for _, n := range path {
				messages = append(messages, engine.Message{
					Role:    n.Role,
					Content: n.Content,
				})
			}

			resp, err := provider.GenerateResponse(context.Background(), messages, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error generating response: %v\n", err)
				os.Exit(1)
			}

			assistantNode, err := mgr.CreateAssistantNode(finalID, resp.Content, resp.ToolCalls)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error saving assistant node: %v\n", err)
				os.Exit(1)
			}
			finalID = assistantNode.ID
		}

		fmt.Println(finalID)
		os.Exit(0)
	}

	// Initialize TUI Model
	startID := lastID
	if *jumpID != "" {
		startID = *jumpID
	}

	m := tui.NewModel(cfg, graph, storage, provider, startID)
	mPtr := &m

	// Start Bubble Tea
	p := tea.NewProgram(mPtr)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there was an error running the TUI: %v", err)
		os.Exit(1)
	}
}
