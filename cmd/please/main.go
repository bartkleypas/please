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
	flag.Parse()

	if !*chatFlag {
		fmt.Println("Usage: please [options]")
		fmt.Println("Options:")
		flag.PrintDefaults()
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

	// Start Bubble Tea
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there was an error running the TUI: %v", err)
		os.Exit(1)
	}
}
