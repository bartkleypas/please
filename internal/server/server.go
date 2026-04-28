package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"org.kleypas.please/internal/engine"
)

//go:embed assets/*
var assets embed.FS

// Server manages the HTTP server for graph visualization
type Server struct {
	Manager *engine.Manager
	server  *http.Server
	port    int
	mu      sync.Mutex
	running bool
}

// NewServer creates a new Server instance
func NewServer(mgr *engine.Manager) *Server {
	return &Server{
		Manager: mgr,
	}
}

// Start begins the HTTP server in a goroutine
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running on port %d", s.port)
	}

	s.port = port
	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/api/graph", s.handleGraph)

	// Static Assets
	mux.HandleFunc("/", s.handleIndex)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

	s.running = true
	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
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

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// We return the graph directly. engine.Graph is serializable.
	// The mutex in engine.Graph handles thread-safety during serialization.
	if err := json.NewEncoder(w).Encode(s.Manager.Graph); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Serve index.html for the root path
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

	// For other paths, try to serve from embedded assets
	http.FileServer(http.FS(assets)).ServeHTTP(w, r)
}
