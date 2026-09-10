package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/worktree"
)

type turnResult struct {
	node *engine.Node
	err  error
}

type turnJob struct {
	ctx     context.Context
	req     engine.TurnRequest
	eventCh chan engine.HarnessEvent
	result  chan turnResult
}

// SessionActor provides serialized turn execution for a single named session.
// All prompts, tool calls, and state transitions within the session are executed
// strictly one-at-a-time, eliminating concurrent state tearing and race conditions.
type SessionActor struct {
	sessionID string
	queue     chan *turnJob
	harness   *engine.SessionHarness
	ctx       context.Context
	cancel    context.CancelFunc
}

func newSessionActor(sessionID string, mgr *engine.Manager, provider engine.Provider, cfg *engine.Config, onNodeSaved func(*engine.Node)) *SessionActor {
	ctx, cancel := context.WithCancel(context.Background())
	sessionMgr := mgr

	if cfg != nil && cfg.EnableWorktreeIsolation() && sessionID != "" && sessionID != "main" {
		configDir, _ := engine.GetConfigDir()
		wtMgr := worktree.NewManager(configDir, mgr.WorkspaceDir)
		if wtMgr.IsGitAvailable() && wtMgr.IsGitRepo() {
			if wtDir, _, err := wtMgr.EnsureWorktree(sessionID); err == nil && wtDir != "" {
				sessionMgr = mgr.CloneWithWorkspace(wtDir, mgr.WorkspaceDir)
			}
		}
	}

	harness := engine.NewSessionHarness(sessionMgr, provider, cfg)
	harness.OnNodeSaved = onNodeSaved

	actor := &SessionActor{
		sessionID: sessionID,
		queue:     make(chan *turnJob, 32),
		harness:   harness,
		ctx:       ctx,
		cancel:    cancel,
	}

	go actor.loop()
	return actor
}

func (a *SessionActor) loop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case job, ok := <-a.queue:
			if !ok {
				return
			}
			node, err := a.harness.ExecuteTurn(job.ctx, job.req, job.eventCh)
			close(job.eventCh)
			job.result <- turnResult{node: node, err: err}
			close(job.result)
		}
	}
}

// Submit enqueues a turn job to be executed sequentially by this session's worker.
func (a *SessionActor) Submit(ctx context.Context, req engine.TurnRequest, eventCh chan engine.HarnessEvent) (<-chan turnResult, error) {
	select {
	case <-a.ctx.Done():
		return nil, fmt.Errorf("session actor %q is stopped", a.sessionID)
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	job := &turnJob{
		ctx:     ctx,
		req:     req,
		eventCh: eventCh,
		result:  make(chan turnResult, 1),
	}

	select {
	case a.queue <- job:
		return job.result, nil
	case <-a.ctx.Done():
		return nil, fmt.Errorf("session actor %q is stopped", a.sessionID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stop terminates the session actor worker.
func (a *SessionActor) Stop() {
	a.cancel()
}

// SessionActorRegistry manages the lifecycle of SessionActors across active sessions.
type SessionActorRegistry struct {
	mu          sync.RWMutex
	actors      map[string]*SessionActor
	manager     *engine.Manager
	provider    engine.Provider
	config      *engine.Config
	onNodeSaved func(*engine.Node)
}

// NewSessionActorRegistry initializes a registry for session actors.
func NewSessionActorRegistry(mgr *engine.Manager, provider engine.Provider, cfg *engine.Config, onNodeSaved func(*engine.Node)) *SessionActorRegistry {
	return &SessionActorRegistry{
		actors:      make(map[string]*SessionActor),
		manager:     mgr,
		provider:    provider,
		config:      cfg,
		onNodeSaved: onNodeSaved,
	}
}

// GetOrCreate returns an existing SessionActor or instantiates a new one if needed.
func (r *SessionActorRegistry) GetOrCreate(sessionID string) *SessionActor {
	if sessionID == "" {
		sessionID = "main"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if actor, ok := r.actors[sessionID]; ok {
		return actor
	}

	actor := newSessionActor(sessionID, r.manager, r.provider, r.config, r.onNodeSaved)
	r.actors[sessionID] = actor
	return actor
}

// SetProvider updates the underlying LLM provider across all session harnesses.
func (r *SessionActorRegistry) SetProvider(provider engine.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provider = provider
	for _, actor := range r.actors {
		actor.harness.Provider = provider
	}
}

// SetConfig updates the config across all session harnesses.
func (r *SessionActorRegistry) SetConfig(cfg *engine.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = cfg
	for _, actor := range r.actors {
		actor.harness.Config = cfg
	}
}

// StopAll gracefully shuts down all running session actors.
func (r *SessionActorRegistry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, actor := range r.actors {
		actor.Stop()
	}
	r.actors = make(map[string]*SessionActor)
}
