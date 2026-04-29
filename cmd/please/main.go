package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"org.kleypas.please/internal/engine"
	"org.kleypas.please/internal/server"
	"org.kleypas.please/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

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

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🦉 Please: A DAG-based TUI for branching LLM conversations.\n\n")
		fmt.Fprintf(os.Stderr, "Usage: please [options] [message...]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -c, --chat          Start the TUI chat interface\n")
		fmt.Fprintf(os.Stderr, "  -v, --vault <path>  Path to a custom vault file\n")
		fmt.Fprintf(os.Stderr, "  -p, --parent <id>   Parent node ID for new message\n")
		fmt.Fprintf(os.Stderr, "  -j, --jump <id>     Node ID to jump to in interactive mode\n")
		fmt.Fprintf(os.Stderr, "  -s, --server <port> Start visualization server on port\n")
		fmt.Fprintf(os.Stderr, "      --no-gen        Disable auto-generation when passing a message\n")
		fmt.Fprintf(os.Stderr, "      --role <role>   Override role (user, tool, system, assistant)\n")
		fmt.Fprintf(os.Stderr, "\nCommands inside the TUI:\n")
		fmt.Fprintf(os.Stderr, "  /help    Show internal command list\n")
		fmt.Fprintf(os.Stderr, "  /map     Visualize conversation branches\n")
		fmt.Fprintf(os.Stderr, "  /server  Control the web visualization server\n")
	}

	flag.Parse()

	// Load Configuration
	cfg, err := engine.LoadConfig()
	if err != nil {
		fmt.Printf("Configuration error: %v\n", err)
		os.Exit(1)
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

	provider := engine.NewOllamaProvider(cfg.Endpoint, cfg.Model)
	mgr := engine.NewManager(graph, storage)
	webServer := server.NewServer(mgr)

	// Determine if we have a message from args or stdin
	var content string
	var inferredRole engine.Role = engine.RoleUser

	if flag.NArg() > 0 {
		content = strings.Join(flag.Args(), " ")
		inferredRole = engine.RoleUser
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Data is being piped in
			bytes, err := io.ReadAll(os.Stdin)
			if err == nil {
				content = strings.TrimSpace(string(bytes))
				inferredRole = engine.RoleTool // Piped input defaults to tool role
			}
		}
	}

	// If explicit role is provided, use it
	if *roleStr != "" {
		inferredRole = engine.Role(*roleStr)
	}

	// Handle Message Mode
	if content != "" {
		parentID := *parent
		if parentID == "" {
			parentID = lastID
		}

		newNode, err := mgr.CreateNode(parentID, inferredRole, content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating node: %v\n", err)
			os.Exit(1)
		}

		finalID := newNode.ID

		if !*noGen {
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
