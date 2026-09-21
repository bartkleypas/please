package engine

import (
	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/providers"
)

// CurrentConfigVersion is the current schema version for config.json
const CurrentConfigVersion = config.CurrentConfigVersion

// ModelOptions holds model inference and sampling parameters (aliased from internal/providers)
type ModelOptions = providers.ModelOptions

// ServerConfig holds settings for running the engine daemon / standalone backend
type ServerConfig = config.ServerConfig

// ClientConfig holds settings for connecting the TUI to a remote daemon
type ClientConfig = config.ClientConfig

// Config is the top-level configuration container (v2 schema)
type Config = config.Config

// Forwarded constructors and helper functions for backward compatibility
var (
	// NewDefaultConfig returns a freshly initialized default configuration
	NewDefaultConfig = config.NewDefaultConfig

	// LoadConfig attempts to load the config from the user's config directory
	LoadConfig = config.LoadConfig

	// LoadConfigFile parses and validates a configuration file at an explicit path
	LoadConfigFile = config.LoadConfigFile

	// GetConfigDir returns the path to the please configuration directory
	GetConfigDir = config.GetConfigDir

	// GetGlobalPleaseDir returns the universal user-level Please directory (~/.please)
	GetGlobalPleaseDir = config.GetGlobalPleaseDir

	// GetWorkspacePleaseDir returns the workspace-local Please directory if present
	GetWorkspacePleaseDir = config.GetWorkspacePleaseDir
)
