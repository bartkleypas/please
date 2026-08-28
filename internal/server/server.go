package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bartkleypas/please/internal/engine"
)

//go:embed assets/*
var assets embed.FS

// Server manages the HTTP and SSE server for graph visualization and API interaction
type Server struct {
	Manager   *engine.Manager
	Provider  engine.LLMProvider
	Config    *engine.Config
	AuthToken string
	EventBus  *EventBus
	server    *http.Server
	host      string
	port      int
	isTLS     bool
	mu        sync.Mutex
	running   bool
}

// NewServer creates a new Server instance
func NewServer(mgr *engine.Manager) *Server {
	return &Server{
		Manager:  mgr,
		EventBus: NewEventBus(),
		host:     "127.0.0.1",
	}
}

// NewServerWithProvider creates a Server instance with provider and configuration
func NewServerWithProvider(mgr *engine.Manager, provider engine.LLMProvider, cfg *engine.Config) *Server {
	token := ""
	if cfg != nil && cfg.Server != nil {
		token = cfg.Server.AuthToken
		if cfg.Server.Options != nil && cfg.Server.Options.NumCtx != nil && mgr != nil {
			mgr.NumCtx = *cfg.Server.Options.NumCtx
		}
	}
	return &Server{
		Manager:   mgr,
		Provider:  provider,
		Config:    cfg,
		AuthToken: token,
		EventBus:  NewEventBus(),
		host:      "127.0.0.1",
	}
}

// SetProvider updates the LLMProvider on the server
func (s *Server) SetProvider(p engine.LLMProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Provider = p
}

// SetConfig updates the configuration on the server
func (s *Server) SetConfig(cfg *engine.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Config = cfg
	if cfg != nil && cfg.Server != nil && cfg.Server.AuthToken != "" {
		s.AuthToken = cfg.Server.AuthToken
	}
}

// SetAuthToken configures a pre-shared bearer token for request authentication
func (s *Server) SetAuthToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AuthToken = token
}

// Handler constructs the configured http.Handler with all middlewares and routes
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// REST API v1
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/status", s.handleHealth)
	mux.HandleFunc("/api/v1/graph", s.handleGraph)
	mux.HandleFunc("/api/v1/events", s.handleEventsStream)
	mux.HandleFunc("/api/v1/nodes", s.handleNodes)
	mux.HandleFunc("/api/v1/nodes/", s.handleNodeByID)
	mux.HandleFunc("/api/v1/branches/", s.handleBranchByID)
	mux.HandleFunc("/api/v1/path/", s.handleBranchByID)
	mux.HandleFunc("/api/v1/supernodes", s.handleSupernodes)
	mux.HandleFunc("/api/v1/gc", s.handleGC)
	mux.HandleFunc("/api/v1/tools", s.handleTools)
	mux.HandleFunc("/api/v1/chat/stream", s.handleChatStream)

	// Legacy endpoints for backward compatibility with visualizer
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/image", s.handleImage)
	mux.HandleFunc("/", s.handleIndex)

	return s.corsMiddleware(s.authMiddleware(mux))
}

// Start begins the HTTP server on 127.0.0.1:port
func (s *Server) Start(port int) error {
	return s.StartWithHost(port, "127.0.0.1")
}

// StartWithHost begins the HTTP server on host:port
func (s *Server) StartWithHost(port int, host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running on %s:%d", s.host, s.port)
	}

	if host == "" {
		host = "127.0.0.1"
	}
	s.port = port
	s.host = host
	s.isTLS = false

	s.server = &http.Server{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Handler:  s.Handler(),
		ErrorLog: log.New(io.Discard, "", 0),
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	s.running = true
	return nil
}

// StartTLS begins the HTTPS server using the provided cert and key files
func (s *Server) StartTLS(port int, host string, certFile, keyFile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running on %s:%d", s.host, s.port)
	}

	if host == "" {
		host = "127.0.0.1"
	}
	s.port = port
	s.host = host
	s.isTLS = true

	s.server = &http.Server{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Handler:  s.Handler(),
		ErrorLog: log.New(io.Discard, "", 0),
	}

	go func() {
		if err := s.server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "TLS Server error: %v\n", err)
		}
	}()

	s.running = true
	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return err
	}

	s.running = false
	return nil
}

// Status returns the current running state and port
func (s *Server) Status() (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.port
}

// DetailStatus returns running state, port, host, and whether TLS is enabled
func (s *Server) DetailStatus() (bool, int, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.port, s.host, s.isTLS
}

// --- Middlewares ---

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health check, index, and legacy images bypass auth
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}

		s.mu.Lock()
		token := s.AuthToken
		s.mu.Unlock()

		if token == "" {
			// No token configured: allow request
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization: Bearer <token>
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			if strings.TrimPrefix(authHeader, "Bearer ") == token {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check query param ?token=<token>
		if r.URL.Query().Get("token") == token {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Unauthorized: invalid or missing Bearer token", http.StatusUnauthorized)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// --- REST Endpoint Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	provider := "unknown"
	model := "unknown"
	if s.Config != nil && s.Config.Server != nil {
		provider = s.Config.Server.Provider
		model = s.Config.Server.Model
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"version":  engine.Version,
		"provider": provider,
		"model":    model,
		"time":     time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, _, err := s.Manager.Sync(); err != nil {
		http.Error(w, "Failed to synchronize graph: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(s.Manager.Graph); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	if s.EventBus == nil {
		s.mu.Lock()
		if s.EventBus == nil {
			s.EventBus = NewEventBus()
		}
		s.mu.Unlock()
	}

	sub := s.EventBus.Subscribe()
	defer sub.Close()

	ctx := r.Context()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-sub.Events:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(data)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		if _, _, err := s.Manager.Sync(); err != nil {
			http.Error(w, "Failed to sync: "+err.Error(), http.StatusInternalServerError)
			return
		}
		nodes := s.Manager.Graph.GetAllNodes()
		if nodes == nil {
			nodes = []*engine.Node{}
		}
		_ = json.NewEncoder(w).Encode(nodes)
		return

	case http.MethodPost:
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		var node engine.Node
		if err := json.Unmarshal(bodyBytes, &node); err == nil && node.ID != "" {
			// Complete node upsert
			if err := s.Manager.Storage.SaveNode(&node); err != nil {
				http.Error(w, "Failed to save node: "+err.Error(), http.StatusInternalServerError)
				return
			}
			s.Manager.Graph.AddNode(&node)

			if s.EventBus != nil {
				s.EventBus.Publish(EventNodeSaved, map[string]interface{}{
					"node_id":   node.ID,
					"parent_id": node.ParentID,
					"role":      node.Role,
				})
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(node)
			return
		}

		var payload struct {
			ParentID string      `json:"parent_id"`
			Role     engine.Role `json:"role"`
			Content  string      `json:"content"`
			Internal bool        `json:"internal"`
		}

		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		if payload.Role == "" {
			payload.Role = engine.RoleUser
		}

		createdNode, err := s.Manager.CreateNode(payload.ParentID, payload.Role, payload.Content, payload.Internal)
		if err != nil {
			http.Error(w, "Failed to create node: "+err.Error(), http.StatusBadRequest)
			return
		}

		if s.EventBus != nil {
			s.EventBus.Publish(EventNodeSaved, map[string]interface{}{
				"node_id":   createdNode.ID,
				"parent_id": createdNode.ParentID,
				"role":      createdNode.Role,
			})
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createdNode)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	id = strings.TrimSuffix(id, "/prune")

	if id == "" {
		http.Error(w, "Missing node ID", http.StatusBadRequest)
		return
	}

	// Check if this is a POST to /api/v1/nodes/{id}/prune
	if strings.HasSuffix(r.URL.Path, "/prune") && r.Method == http.MethodPost {
		if err := s.Manager.PruneBranch(id); err != nil {
			http.Error(w, "Failed to prune branch: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if s.EventBus != nil {
			s.EventBus.Publish(EventBranchPruned, map[string]interface{}{"root_id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pruned", "node_id": id})
		return
	}

	switch r.Method {
	case http.MethodGet:
		node, err := s.Manager.GetNode(id)
		if err != nil {
			http.Error(w, "Node not found: "+err.Error(), http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(node)

	case http.MethodDelete:
		if err := s.Manager.PruneBranch(id); err != nil {
			http.Error(w, "Failed to prune branch: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if s.EventBus != nil {
			s.EventBus.Publish(EventBranchPruned, map[string]interface{}{"root_id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pruned", "node_id": id})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBranchByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/branches/")
	id = strings.TrimPrefix(id, "/api/v1/path/")

	if id == "" {
		http.Error(w, "Missing node ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		path, err := s.Manager.GetPath(id)
		if err != nil {
			http.Error(w, "Path not found: "+err.Error(), http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(path)
		return

	case http.MethodDelete, http.MethodPost:
		if err := s.Manager.PruneBranch(id); err != nil {
			http.Error(w, "Failed to prune branch: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if s.EventBus != nil {
			s.EventBus.Publish(EventBranchPruned, map[string]interface{}{"root_id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pruned", "branch_id": id})
		return

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSupernodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var payload struct {
		NodeIDs   []string `json:"node_ids"`
		Directive string   `json:"directive,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(payload.NodeIDs) == 0 {
		http.Error(w, "node_ids array cannot be empty", http.StatusBadRequest)
		return
	}

	if s.Provider == nil {
		http.Error(w, "LLM provider is not configured", http.StatusInternalServerError)
		return
	}

	superNode, err := s.Manager.CompactRangeWithDirective(r.Context(), s.Provider, payload.NodeIDs, payload.Directive)
	if err != nil {
		http.Error(w, "Failed to create supernode: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if s.EventBus != nil {
		s.EventBus.Publish(EventBranchCompacted, map[string]interface{}{
			"supernode_id": superNode.ID,
			"parent_id":    superNode.ParentID,
		})
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(superNode)
}

func (s *Server) handleGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	count, err := s.Manager.GarbageCollect()
	if err != nil {
		http.Error(w, "Garbage collection failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "success",
		"purged_records": count,
	})
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type toolInfo struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	}

	var list []toolInfo
	if s.Manager.Registry != nil {
		for _, t := range s.Manager.Registry.Tools {
			list = append(list, toolInfo{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
	}

	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		data, err := assets.ReadFile("assets/index.html")
		if err != nil {
			http.Error(w, "Could not read index.html", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	http.FileServer(http.FS(assets)).ServeHTTP(w, r)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	imagePath := r.URL.Query().Get("path")
	if imagePath == "" {
		http.Error(w, "Missing 'path' parameter", http.StatusBadRequest)
		return
	}

	stat, err := os.Stat(imagePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if stat.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	mimeType := "application/octet-stream"
	ext := strings.ToLower(filepath.Ext(imagePath))
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	case ".svg":
		mimeType = "image/svg+xml"
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Write(data)
}
