---
type: Decision
title: "ADR 008: Modular Configuration Subsystem Extraction"
description: "Extraction of configuration models, schema migration, directory resolution, and options parsing from internal/engine into a dedicated internal/config package."
tags:
  - please
  - architecture
  - adr
  - config
  - decoupling
timestamp: "2026-09-07T18:35:00-07:00"
---

# ADR 008: Modular Configuration Subsystem Extraction

## Status

Accepted

## Context

Following the extraction of tools ([ADR 005](005-modular-tools-extraction.md)) and providers ([ADR 006](006-modular-providers-extraction.md)), `internal/engine` continues to house the application configuration subsystem in [internal/engine/config.go](../internal/engine/config.go) (410 lines, plus 237 lines of tests).

### The Inversion Problem
Configuration represents the outermost declarative specification of how Please runs:
* Configuration schemas (`Config`, `ServerConfig`, `ClientConfig`)
* Schema migrations (converting legacy flat v1 configs to modern v2 blocks)
* OS directory discovery (`GetConfigDir()`, mapping `PLEASE_CONFIG_DIR`, `os.UserConfigDir()`, and macOS `~/Library/Application Support`)
* Path and environment expansion (`$HOME`, `~`, `${VAR}`)
* Model options and sampling limits (`ModelOptions`, `NumCtx`, `Temperature`, `TopP`)

Currently, external packages such as `cmd/please`, `internal/server`, and `internal/tui` must import the heavy `internal/engine` package simply to load a configuration file or inspect a directory path. This creates an unnecessary inverted dependency where low-level declarative configuration is bound to cognitive execution runtimes.

### Dependency Analysis

Inspecting `config.go` reveals that it depends only on:
1. `internal/providers` (for `ModelOptions`)
2. The Go standard library (`encoding/json`, `fmt`, `os`, `path/filepath`, `strings`)

It has **zero** dependencies on `Graph`, `Node`, `Storage`, `Service`, `Tools`, or SQLite. It is a pure leaf abstraction.

---

## Decision

We will extract all configuration structures, file loaders, directory resolvers, and migration routines from `internal/engine` into a dedicated package: **`internal/config`**.

### Target Package Structure

```
internal/config/
├── config.go        # Config, ServerConfig, ClientConfig, CurrentConfigVersion, defaults
├── dir.go           # GetConfigDir, OS-specific directory discovery, environment resolution
├── migrate.go       # v1 -> v2 JSON schema migration logic
└── config_test.go   # Unit tests for loading, migration, directory resolution
```

### Dependency Flow

```
                 internal/tools (leaf)
                       ▲
                       │
                internal/providers
                       ▲
                       │
                internal/config (leaf for engine)
                       ▲
                       │
                internal/engine
```

### Engine Backward Compatibility via Type Aliases

To guarantee zero breaking changes across `internal/server`, `internal/tui`, and `cmd/please`, `internal/engine` will re-export all configuration types using Go type aliases and forwarding functions:

```go
package engine

import "github.com/bartkleypas/please/internal/config"

type Config = config.Config
type ServerConfig = config.ServerConfig
type ClientConfig = config.ClientConfig

var NewDefaultConfig = config.NewDefaultConfig
var LoadConfigFile = config.LoadConfigFile
var GetConfigDir = config.GetConfigDir
```

---

## Consequences

### Positive
* **Decoupled Architecture**: `cmd/please`, `internal/server`, and `internal/tui` can parse, validate, and manipulate configurations without pulling in the cognitive engine or SQLite drivers.
* **Prepares for Multi-Worktree Configuration**: Enables lightweight config cloning and session-level overrides required by [ADR 007](007-multi-session-daemon-branch-concurrency.md).
* **High Test Velocity**: Config parsing and migration unit tests can execute in complete isolation from storage vaults.

### Negative
* Introduces one additional package directory in `internal/`.
