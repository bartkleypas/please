package acp

import (
	"context"
	"io"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/engine"
	"github.com/bartkleypas/please/internal/providers"
	acpsdk "github.com/coder/acp-go-sdk"
)

// Run initializes an Agent and connects it to the peer over the provided I/O streams.
// In ACP transport conventions, out receives agent writes (to client) and in provides client reads.
// Run blocks until the connection terminates or ctx is cancelled.
func Run(ctx context.Context, mgr *engine.Manager, provider providers.Provider, cfg *config.Config, in io.Reader, out io.Writer) error {
	workspaceDir := ""
	if cfg != nil {
		workspaceDir = cfg.GetWorkspaceDir()
	}
	agent := NewAgent(mgr, provider, cfg, workspaceDir)

	conn := acpsdk.NewAgentSideConnection(agent, out, in)
	agent.SetConnection(conn)

	select {
	case <-conn.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
