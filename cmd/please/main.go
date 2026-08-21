package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	// Subcommand routing
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			runServe(os.Args[2:])
			return
		case "connect":
			runConnect(os.Args[2:])
			return
		case "cert":
			if len(os.Args) > 2 && os.Args[2] == "generate" {
				runCertGenerate(os.Args[3:])
				return
			}
			fmt.Fprintf(os.Stderr, "Usage: please cert generate [options]\n")
			os.Exit(1)
		}
	}

	// Default CLI / TUI flag parsing
	chatFlag := flag.Bool("chat", false, "Start the TUI chat interface")
	flag.BoolVar(chatFlag, "c", false, "Start the TUI chat interface (shorthand)")

	vaultPath := flag.String("vault", "", "Path to a custom vault.jsonl or .db file")
	flag.StringVar(vaultPath, "v", "", "Path to a custom vault file (shorthand)")

	workspacePath := flag.String("workspace", "", "Path to the project workspace directory")
	flag.StringVar(workspacePath, "w", "", "Path to the project workspace directory (shorthand)")

	parent := flag.String("parent", "", "Parent node ID for new message")
	flag.StringVar(parent, "p", "", "Parent node ID (shorthand)")

	jumpID := flag.String("jump", "", "Node ID to jump to in interactive mode")
	flag.StringVar(jumpID, "j", "", "Node ID to jump to in interactive mode (shorthand)")

	serverPort := flag.Int("server", 0, "Start API & visualization server on specified port")
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
		fmt.Fprintf(os.Stderr, "🦉 Please: A DAG-based conversation harness, streaming daemon, and client.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  please                    Start standalone interactive TUI (default)\n")
		fmt.Fprintf(os.Stderr, "  please serve [options]    Start the API & streaming engine daemon\n")
		fmt.Fprintf(os.Stderr, "  please connect [url]      Connect TUI to a remote Please daemon\n")
		fmt.Fprintf(os.Stderr, "  please cert generate      Generate 20-year internal Root CA and Server certificates\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -v, --vault <path>     Path to a custom vault file\n")
		fmt.Fprintf(os.Stderr, "  -w, --workspace <path> Path to project workspace directory\n")
		fmt.Fprintf(os.Stderr, "  -p, --parent <id>      Parent node ID for new message\n")
		fmt.Fprintf(os.Stderr, "  -j, --jump <id>        Node ID to jump to in interactive mode\n")
		fmt.Fprintf(os.Stderr, "  -s, --server <port>    Start API & visualization server on port alongside TUI\n")
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

	if *workspacePath != "" {
		cfg.Server.WorkspaceDir = *workspacePath
	}

	// Apply CLI flag overrides to cfg.Server.Options
	if *tempFlag >= 0 || *topPFlag >= 0 || *topKFlag >= 0 || *ctxFlag > 0 || *maxTokensFlag > 0 {
		if cfg.Server.Options == nil {
			cfg.Server.Options = &engine.ModelOptions{}
		}
		if *tempFlag >= 0 {
			cfg.Server.Options.Temperature = tempFlag
		}
		if *topPFlag >= 0 {
			cfg.Server.Options.TopP = topPFlag
		}
		if *topKFlag >= 0 {
			cfg.Server.Options.TopK = topKFlag
		}
		if *ctxFlag > 0 {
			cfg.Server.Options.NumCtx = ctxFlag
		}
		if *maxTokensFlag > 0 {
			cfg.Server.Options.MaxTokens = maxTokensFlag
		}
	}

	// Initialize Storage
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
			fmt.Printf("Error initializing sqlite storage: %v\n", err)
			os.Exit(1)
		}
	} else {
		storage = engine.NewJSONLStorage(finalVaultPath, cfg.Server.EncryptionKey)
	}

	graph, lastID, err := storage.LoadGraph()
	if err != nil {
		fmt.Printf("Error loading graph: %v\n", err)
		os.Exit(1)
	}

	var provider engine.LLMProvider
	if cfg.Server.Provider == "openai" {
		provider = engine.NewOpenAIProvider(cfg.Server.Endpoint, cfg.Server.Model, cfg.Server.APIKey, cfg.Server.Options)
	} else {
		provider = engine.NewOllamaProvider(cfg.Server.Endpoint, cfg.Server.Model, cfg.Server.Options)
	}
	mgr := engine.NewManager(graph, storage)
	mgr.RegisterDefaultTools(cfg.GetWorkspaceDir())
	webServer := server.NewServerWithProvider(mgr, provider, cfg)

	// Determine if we have a message from args or stdin
	var pipedContent string
	var pipedRole engine.Role = engine.RoleTool

	stat, statErr := os.Stdin.Stat()
	if statErr == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		bytes, err := io.ReadAll(os.Stdin)
		if err == nil {
			pipedContent = strings.TrimSpace(string(bytes))
		}
	}

	var argContent string
	if flag.NArg() > 0 {
		argContent = strings.Join(flag.Args(), " ")
	}

	if *roleStr != "" {
		pipedRole = engine.Role(*roleStr)
	}

	// Handle Message Mode
	if pipedContent != "" || argContent != "" {
		parentID := *parent
		if parentID == "" {
			if pipedContent != "" && pipedRole == engine.RoleSystem {
				parentID = ""
			} else {
				parentID = lastID
			}
		}

		var finalID string

		if pipedContent != "" {
			newNode, err := mgr.CreateNode(parentID, pipedRole, pipedContent, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating piped node: %v\n", err)
				os.Exit(1)
			}
			finalID = newNode.ID
			parentID = finalID
		}

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

	// Handle legacy server flag
	if *serverPort > 0 {
		if err := webServer.Start(*serverPort); err != nil {
			fmt.Printf("Error starting web server: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Web server started on http://localhost:%d\n", *serverPort)
	}

	// Start Standalone TUI
	startID := lastID
	if *jumpID != "" {
		startID = *jumpID
	}

	m := tui.NewModel(cfg, graph, storage, provider, startID)
	m.Server = webServer
	p := tea.NewProgram(&m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there was an error running the TUI: %v\n", err)
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "Port to listen on")
	fs.IntVar(port, "p", 8080, "Port to listen on (shorthand)")
	host := fs.String("host", "127.0.0.1", "Host address to bind to")
	fs.StringVar(host, "H", "127.0.0.1", "Host address to bind to (shorthand)")

	tlsFlag := fs.Bool("tls", false, "Enable TLS (HTTPS)")
	certFile := fs.String("cert", "", "Path to TLS certificate file")
	keyFile := fs.String("key", "", "Path to TLS private key file")
	genCerts := fs.Bool("generate-certs", false, "Automatically generate 20-year internal certificates if missing")
	tokenFlag := fs.String("token", "", "Pre-shared bearer token for authentication")
	vaultPath := fs.String("vault", "", "Path to vault file")
	workspacePath := fs.String("workspace", "", "Path to workspace directory")

	_ = fs.Parse(args)

	cfg, err := engine.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	if *workspacePath != "" {
		cfg.Server.WorkspaceDir = *workspacePath
	}
	if *tokenFlag != "" {
		cfg.Server.AuthToken = *tokenFlag
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
	mgr.RegisterDefaultTools(cfg.GetWorkspaceDir())

	srv := server.NewServerWithProvider(mgr, provider, cfg)
	if cfg.Server.AuthToken != "" {
		srv.SetAuthToken(cfg.Server.AuthToken)
	}

	protocol := "http"
	if *tlsFlag {
		protocol = "https"
		cFile := *certFile
		kFile := *keyFile

		if cFile == "" && cfg.Server.TLSCertFile != "" {
			cFile = cfg.Server.TLSCertFile
			kFile = cfg.Server.TLSKeyFile
		}

		if cFile == "" || kFile == "" || *genCerts {
			cfgDir, _ := engine.GetConfigDir()
			certDir := filepath.Join(cfgDir, "certs")
			if _, statErr := os.Stat(filepath.Join(certDir, "server.crt")); os.IsNotExist(statErr) || *genCerts {
				bundle, genErr := server.Generate20YearCerts(certDir, []string{*host, "localhost", "127.0.0.1", "please.local"})
				if genErr != nil {
					fmt.Fprintf(os.Stderr, "Failed to generate certificates: %v\n", genErr)
					os.Exit(1)
				}
				cFile = bundle.ServerCertPath
				kFile = bundle.ServerKeyPath
				fmt.Printf("🔐 Generated 20-year internal certificates in %s\n", certDir)
			} else {
				cFile = filepath.Join(certDir, "server.crt")
				kFile = filepath.Join(certDir, "server.key")
			}
		}

		if err := srv.StartTLS(*port, *host, cFile, kFile); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start TLS server: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := srv.StartWithHost(*port, *host); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
			os.Exit(1)
		}
	}

	authStatus := "disabled (open local)"
	if cfg.Server.AuthToken != "" {
		authStatus = "enabled (Bearer token active)"
	}

	fmt.Printf("\n🦉 Please Engine Daemon v%s\n", engine.Version)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  • Endpoint:       %s://%s:%d\n", protocol, *host, *port)
	fmt.Printf("  • Vault:          %s (%s)\n", finalVaultPath, storageType)
	fmt.Printf("  • Provider:       %s (%s)\n", cfg.Server.Provider, cfg.Server.Model)
	fmt.Printf("  • Authentication: %s\n", authStatus)
	fmt.Printf("  • SSE Stream:     %s://%s:%d/api/v1/chat/stream\n", protocol, *host, *port)
	fmt.Printf("  • Graph REST:     %s://%s:%d/api/v1/graph\n", protocol, *host, *port)
	fmt.Printf("  • Web View:       %s://%s:%d/\n", protocol, *host, *port)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Ready for client connections. Press Ctrl+C to stop.\n\n")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down engine daemon gracefully...")
	_ = srv.Stop()
	fmt.Println("Server stopped.")
}

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	tokenFlag := fs.String("token", "", "Bearer token for daemon authentication")
	caCertFlag := fs.String("ca-cert", "", "Path to root CA certificate for TLS verification")
	jumpID := fs.String("jump", "", "Node ID to jump to in interactive mode")
	fs.StringVar(jumpID, "j", "", "Node ID to jump to in interactive mode (shorthand)")

	_ = fs.Parse(args)

	cfg, err := engine.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	remoteURL := "http://127.0.0.1:8080"
	if cfg.Client != nil && cfg.Client.RemoteURL != "" {
		remoteURL = cfg.Client.RemoteURL
	}
	if fs.NArg() > 0 {
		remoteURL = fs.Arg(0)
	}

	token := ""
	if cfg.Client != nil && cfg.Client.AuthToken != "" {
		token = cfg.Client.AuthToken
	}
	if *tokenFlag != "" {
		token = *tokenFlag
	}

	caCert := ""
	if cfg.Client != nil && cfg.Client.CACertPath != "" {
		caCert = cfg.Client.CACertPath
	}
	if *caCertFlag != "" {
		caCert = *caCertFlag
	}

	// 1. Create RemoteDaemonProvider
	provider, err := engine.NewRemoteDaemonProvider(remoteURL, token, caCert)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize remote provider: %v\n", err)
		os.Exit(1)
	}

	// 2. Pull initial graph from daemon
	graph, lastID, err := fetchRemoteGraph(remoteURL, token, caCert)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to daemon at %s: %v\n", remoteURL, err)
		fmt.Fprintf(os.Stderr, "Ensure the daemon is running with: please serve\n")
		os.Exit(1)
	}

	// 3. Setup lightweight in-memory storage for local TUI caching
	storage := engine.NewJSONLStorage("", "")

	startID := lastID
	if *jumpID != "" {
		startID = *jumpID
	}

	m := tui.NewModel(cfg, graph, storage, provider, startID)
	m.RemoteURL = remoteURL
	p := tea.NewProgram(&m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

func fetchRemoteGraph(baseURL, token, caCertPath string) (*engine.Graph, string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	if caCertPath != "" {
		caPEM, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read CA certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, "", fmt.Errorf("failed to parse CA certificate")
		}
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		}
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/graph", nil)
	if err != nil {
		return nil, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	var graph engine.Graph
	if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
		return nil, "", fmt.Errorf("failed to decode graph JSON: %w", err)
	}

	// Find active leaf (latest timestamp)
	var latestID string
	var latestTime time.Time
	for id, node := range graph.Nodes {
		if node.Timestamp.After(latestTime) {
			latestTime = node.Timestamp
			latestID = id
		}
	}

	return &graph, latestID, nil
}

func runCertGenerate(args []string) {
	fs := flag.NewFlagSet("cert generate", flag.ExitOnError)
	dir := fs.String("dir", "", "Output directory for certificates")
	hosts := fs.String("hosts", "localhost,127.0.0.1,please.local", "Comma-separated hostnames/IPs for SANs")
	_ = fs.Parse(args)

	outDir := *dir
	if outDir == "" {
		cfgDir, err := engine.GetConfigDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not determine config directory: %v\n", err)
			os.Exit(1)
		}
		outDir = filepath.Join(cfgDir, "certs")
	}

	hostList := strings.Split(*hosts, ",")
	bundle, err := server.Generate20YearCerts(outDir, hostList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating certificates: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✨ 20-Year Internal Certificates Generated Successfully!\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  • Root CA Cert:    %s\n", bundle.CACertPath)
	fmt.Printf("  • Root CA Key:     %s (Keep private!)\n", bundle.CAKeyPath)
	fmt.Printf("  • Server Cert:     %s\n", bundle.ServerCertPath)
	fmt.Printf("  • Server Key:      %s\n", bundle.ServerKeyPath)
	fmt.Printf("  • Validity:        20 Years (7,300 Days)\n")
	fmt.Printf("  • SANs:            %s\n", strings.Join(hostList, ", "))
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("To use with daemon:\n")
	fmt.Printf("  please serve --tls --cert %s --key %s\n\n", bundle.ServerCertPath, bundle.ServerKeyPath)
}
