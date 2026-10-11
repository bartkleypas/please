---
type: Concept
title: "Package internal/config Index"
description: "Configuration schemas, .please/ workspace discovery ladder, JSON validation, and Schema v2 migration utilities (ADR-008, ADR-016)."
tags:
  - please
  - config
  - workspace
  - index
timestamp: "2026-10-11T10:22:00-07:00"
---

# Package internal/config Index (`internal/config`)

The `internal/config` package (Layer 1: Infrastructure) manages Schema v2 configuration parsing (`ServerConfig` and `ClientConfig`), workspace root resolution, the `.please/` discovery ladder, and v1-to-v2 schema migrations per [ADR-008](../../decisions/008-modular-config-extraction.md) and [ADR-016](../../decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md).

## Core Files & Components

* [config.go](config.go) - Declares `Config`, `ServerConfig`, and `ClientConfig` (including `context_shaper`, `sandbox_policy`, `signat_steering`, `ambient_telemetry`, `worktree_isolation`, `bell_on_turn_complete`, and `natural_pacing`). Provides workspace and directory discovery helpers (`FindWorkspaceRoot`, `GetWorkspacePleaseDir`, `GetGlobalPleaseDir`, `GetConfigDir`, `LoadConfig`, `LoadConfigFile`).
* [config_test.go](config_test.go) - Hermetic unit tests verifying Schema v1 $\rightarrow$ v2 auto-migration, `.please/` workspace precedence, environment variable isolation (`PLEASE_CONFIG_DIR`), and path expansion.

## Configuration Discovery Ladder ([ADR-016](../../decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md))

1. **Explicit CLI Flag**: `-c / --config <path>` (`LoadConfigFile`).
2. **Environment Override**: `PLEASE_CONFIG_DIR` (used extensively for hermetic test isolation).
3. **Workspace-Local Anchor**: `<workspace-root>/.please/config.json` (scaffolded via `please init`).
4. **Global Universal Anchor**: `~/.please/config.json` (or legacy platform config dir `~/.config/please/config.json` / `~/Library/Application Support/please/config.json`).
5. **Runtime Defaults**: `NewDefaultConfig()` (`gemma4:e4b`, SQLite WAL vault, `sigmoid` context shaper, standard sandbox policy).
