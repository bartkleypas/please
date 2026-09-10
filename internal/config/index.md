---
type: Concept
title: "Package internal/config Index"
description: "Configuration loading, directory discovery, JSON schema parsing, options validation, and migration utilities (ADR-008)."
tags:
  - please
  - config
  - index
timestamp: "2026-09-10T11:42:00-07:00"
---

# Package internal/config Index

The `internal/config` package manages configuration schema parsing, environment defaults, directory discovery across platforms (macOS Application Support, Linux XDG, Windows AppData), and options migration per [ADR-008](../../decisions/008-modular-config-extraction.md).

## Core Files & Components

*   [config.go](config.go) - Declares `Config`, `ModelConfig`, `ProviderConfig`, and workspace resolution functions (`LoadConfig`, `DefaultConfig`, `ConfigDir`).
*   [config_test.go](config_test.go) - Unit tests verifying default value generation, environment variable overrides, and path canonicalization.

## Configuration Resolution Order

1. Command-line flags (e.g. `--config <path>`, `--model <name>`, `--provider <type>`, `--worktree`).
2. Environment variables (`PLEASE_CONFIG`, `PLEASE_MODEL`, `PLEASE_PROVIDER`).
3. Local project configuration (`.please/config.json`).
4. User global configuration (`~/.config/please/config.json` or macOS Application Support).
5. Hardcoded runtime defaults.
