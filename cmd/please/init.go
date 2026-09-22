package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/graph"
	"github.com/bartkleypas/please/internal/storage"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

var (
	initBoxStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e0e7e3")).Background(lipgloss.Color("#1e3a2f")).Padding(0, 1)
	initSuccessStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ade80"))
	initDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	initPathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa"))
)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	globalFlag := fs.Bool("global", false, "Initialize user-global environment (~/.please) instead of workspace")
	fs.BoolVar(globalFlag, "g", false, "Initialize user-global environment (shorthand)")
	wsFlag := fs.String("workspace", "", "Path to workspace directory (defaults to current working directory)")
	fs.StringVar(wsFlag, "w", "", "Path to workspace directory (shorthand)")
	forceFlag := fs.Bool("force", false, "Force re-initialization even if .please already exists")
	fs.BoolVar(forceFlag, "f", false, "Force re-initialization (shorthand)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: please init [options]\n\n")
		fmt.Fprintf(os.Stderr, "Establish a self-contained Please workspace or bootstrap user-global environment (ADR 016).\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -g, --global           Initialize user-global directory (~/.please) instead of workspace\n")
		fmt.Fprintf(os.Stderr, "  -w, --workspace <path> Target workspace directory (default: current directory or git root)\n")
		fmt.Fprintf(os.Stderr, "  -f, --force            Force re-scaffolding of existing environment\n")
	}

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
		os.Exit(1)
	}

	if *globalFlag {
		if err := executeGlobalInit(*forceFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing global environment: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := executeWorkspaceInit(*wsFlag, *forceFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing workspace: %v\n", err)
		os.Exit(1)
	}
}

func executeWorkspaceInit(workspacePath string, force bool) error {
	wsRoot, _ := config.FindWorkspaceRoot(workspacePath)
	if resolved, err := filepath.EvalSymlinks(wsRoot); err == nil {
		wsRoot = resolved
	}

	pleaseDir := filepath.Join(wsRoot, ".please")
	configPath := filepath.Join(pleaseDir, "config.json")
	vaultPath := filepath.Join(pleaseDir, "vault.db")

	isReinit := false
	if fi, err := os.Stat(pleaseDir); err == nil && fi.IsDir() && !force {
		isReinit = true
	}

	if err := os.MkdirAll(pleaseDir, 0755); err != nil {
		return fmt.Errorf("failed to create .please directory: %w", err)
	}

	// 1. Scaffold .please/config.json if not present
	configCreated := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) || force {
		cfg := config.NewDefaultConfig()
		cfg.Server.VaultPath = "./.please/vault.db"
		cfg.Server.WorkspaceDir = wsRoot
		if err := cfg.SaveWorkspace(wsRoot); err != nil {
			return fmt.Errorf("failed to write workspace config: %w", err)
		}
		configCreated = true
	}

	// 2. Initialize .please/vault.db (SQLite with WAL mode)
	vaultCreated := false
	if _, err := os.Stat(vaultPath); os.IsNotExist(err) || force {
		store, err := storage.NewSQLiteStorage(vaultPath, "")
		if err != nil {
			return fmt.Errorf("failed to initialize SQLite vault: %w", err)
		}
		g, _, err := store.LoadGraph()
		if err == nil && len(g.Nodes) == 0 {
			// Seed Genesis root node
			genesisPrompt := "You are Please 🦉, a lightweight, highly responsive terminal AI assistant."
			id, _ := uuid.NewV7()
			sysNode := &graph.Node{
				ID:        id.String(),
				Role:      graph.RoleSystem,
				Content:   genesisPrompt,
				Timestamp: time.Now(),
				Metadata:  make(map[string]string),
			}
			_ = store.SaveNode(sysNode)
		}
		vaultCreated = true
	}

	// 3. Git hygiene: automatically inspect and append to .gitignore
	gitIgnored := false
	gitDir := filepath.Join(wsRoot, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		gitignorePath := filepath.Join(wsRoot, ".gitignore")
		if appended, err := ensureGitIgnore(gitignorePath, ".please/"); err == nil && appended {
			gitIgnored = true
		}
	}

	// 4. Terminal Output
	fmt.Println()
	fmt.Println(initBoxStyle.Render("🦉 Please Workspace Initialized"))
	fmt.Println()

	if isReinit {
		fmt.Printf("  %s Workspace already anchored at: %s\n", initDimStyle.Render("•"), initPathStyle.Render(pleaseDir))
	} else {
		fmt.Printf("  %s Created workspace container: %s\n", initSuccessStyle.Render("✔"), initPathStyle.Render(pleaseDir))
	}

	if configCreated {
		fmt.Printf("  %s Seeded workspace configuration: %s\n", initSuccessStyle.Render("✔"), initPathStyle.Render(configPath))
	} else {
		fmt.Printf("  %s Preserved existing configuration: %s\n", initDimStyle.Render("•"), initPathStyle.Render(configPath))
	}

	if vaultCreated {
		fmt.Printf("  %s Initialized conversation vault: %s (WAL mode)\n", initSuccessStyle.Render("✔"), initPathStyle.Render(vaultPath))
	} else {
		fmt.Printf("  %s Preserved existing vault: %s\n", initDimStyle.Render("•"), initPathStyle.Render(vaultPath))
	}

	if gitIgnored {
		fmt.Printf("  %s Added '.please/' to .gitignore\n", initSuccessStyle.Render("✔"))
	}

	fmt.Println()
	fmt.Println(initDimStyle.Render("Run 'please' to start your first session."))
	fmt.Println()

	return nil
}

func executeGlobalInit(force bool) error {
	globalDir, err := config.GetGlobalPleaseDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(globalDir, 0755); err != nil {
		return fmt.Errorf("failed to create global please directory: %w", err)
	}

	configPath := filepath.Join(globalDir, "config.json")
	vaultPath := filepath.Join(globalDir, "vault.db")

	configCreated := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) || force {
		cfg := config.NewDefaultConfig()
		cfg.Server.VaultPath = vaultPath
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("failed to save global config: %w", err)
		}
		configCreated = true
	}

	vaultCreated := false
	if _, err := os.Stat(vaultPath); os.IsNotExist(err) || force {
		store, err := storage.NewSQLiteStorage(vaultPath, "")
		if err != nil {
			return fmt.Errorf("failed to initialize global SQLite vault: %w", err)
		}
		g, _, err := store.LoadGraph()
		if err == nil && len(g.Nodes) == 0 {
			genesisPrompt := "You are Please 🦉, a lightweight, highly responsive terminal AI assistant."
			id, _ := uuid.NewV7()
			sysNode := &graph.Node{
				ID:        id.String(),
				Role:      graph.RoleSystem,
				Content:   genesisPrompt,
				Timestamp: time.Now(),
				Metadata:  make(map[string]string),
			}
			_ = store.SaveNode(sysNode)
		}
		vaultCreated = true
	}

	fmt.Println()
	fmt.Println(initBoxStyle.Render("🦉 Please Global Environment Initialized"))
	fmt.Println()
	fmt.Printf("  %s Global anchor directory: %s\n", initSuccessStyle.Render("✔"), initPathStyle.Render(globalDir))
	if configCreated {
		fmt.Printf("  %s Seeded global configuration: %s\n", initSuccessStyle.Render("✔"), initPathStyle.Render(configPath))
	} else {
		fmt.Printf("  %s Preserved global configuration: %s\n", initDimStyle.Render("•"), initPathStyle.Render(configPath))
	}
	if vaultCreated {
		fmt.Printf("  %s Initialized global vault: %s\n", initSuccessStyle.Render("✔"), initPathStyle.Render(vaultPath))
	} else {
		fmt.Printf("  %s Preserved global vault: %s\n", initDimStyle.Render("•"), initPathStyle.Render(vaultPath))
	}
	fmt.Println()
	return nil
}

func ensureGitIgnore(gitignorePath, entry string) (bool, error) {
	entryClean := strings.TrimSpace(entry)
	if existingData, err := os.ReadFile(gitignorePath); err == nil {
		lines := strings.Split(string(existingData), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == entryClean || trimmed == strings.TrimSuffix(entryClean, "/") {
				// Already ignored
				return false, nil
			}
		}

		// Append to existing .gitignore
		content := string(existingData)
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += fmt.Sprintf("\n# Please agent workspace state and vault (ADR 016)\n%s\n", entryClean)
		return true, os.WriteFile(gitignorePath, []byte(content), 0644)
	}

	// Create new .gitignore
	content := fmt.Sprintf("# Please agent workspace state and vault (ADR 016)\n%s\n", entryClean)
	return true, os.WriteFile(gitignorePath, []byte(content), 0644)
}
