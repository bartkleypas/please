package main

import (
	"flag"
	"fmt"
	"os"

	"org.kleypas.please/internal/engine"
	"org.kleypas.please/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Define flags
	chatFlag := flag.Bool("chat", false, "Start the TUI chat interface")
	flag.BoolVar(chatFlag, "c", false, "Start the TUI chat interface (shorthand)")

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

	if !*chatFlag {
		flag.Usage()
		os.Exit(0)
	}

	// Load Configuration
	cfg, err := engine.LoadConfig()
	if err != nil {
		fmt.Printf("Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Initialize Storage and Graph using the path from config
	storage := engine.NewJSONLStorage(cfg.VaultPath)
	graph, lastID, err := storage.LoadGraph()
	if err != nil {
		fmt.Printf("Error loading graph: %v\n", err)
		os.Exit(1)
	}

	// Initialize LLM Provider using settings from config
	provider := engine.NewOllamaProvider(cfg.Endpoint, cfg.Model)

	// Initialize TUI Model with the last ID retrieved from storage
	m := tui.NewModel(graph, storage, provider, lastID)
	mPtr := &m

	// Start Bubble Tea
	p := tea.NewProgram(mPtr)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there was an error running the TUI: %v", err)
		os.Exit(1)
	}
}
