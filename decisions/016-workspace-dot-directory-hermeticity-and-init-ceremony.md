---
type: Decision
title: "016: Workspace Dot-Directory Hermeticity, Global Anchor, and the 'please init' Ceremony"
description: "Architecture decision record establishing workspace-local .please/ directory hermeticity, the global ~/.please/ anchor, retiring OS-specific Application Support sprawl, aligning physical storage with ADR-014 memory scopes, and introducing the 'please init' ceremony."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - config
  - storage
  - cli
  - ergonomics
  - init
  - memory
timestamp: "2026-09-21T09:45:00-07:00"
---

# ADR 016: Workspace Dot-Directory Hermeticity, Global Anchor, and the 'please init' Ceremony

## Status

Accepted

---

## Context

In the architectural evolution of `please`, configuration and persistence state have historically traversed several disparate locations:

1. **OS-Specific Configuration Sprawl**: 
   Following Go's standard library conventions ([ADR 008](008-modular-config-extraction.md)), `config.LoadConfig()` delegated to `os.UserConfigDir()`. On macOS, this buried configuration in `~/Library/Application Support/please/config.json`. On Linux, it targeted `~/.config/please/config.json`. For terminal-centric operators who live in shells and git repositories, operating system "Application Support" directories feel opaque, non-tactile, and foreign compared to the Unix dotfile convention (`~/.gitconfig`, `~/.cargo/`, `~/.zshrc`).
2. **Unanchored Working Tree Databases**:
   Conversation DAGs and agent memory stores ([ADR 002](002-graph-sqlite-storage.md), [ADR 014](014-persistent-agent-memory-and-cybernetic-recall.md)) defaulted to creating a loose `vault.db` (plus `-wal` and `-shm` sidecars) in the current working directory (`./vault.db`). In active software projects, loose database files clutter root git checkouts and risk accidental commits or git tracking pollution.
3. **Ad-Hoc Candidate Fallback Ladders**:
   To reconcile test fixtures and disparate execution contexts, CLI inspection commands ([ADR 014](014-persistent-agent-memory-and-cybernetic-recall.md), [ADR 015](015-living-scenarios-and-test-subsystem-decomposition.md)) implemented multi-path candidate checking:
   ```go
   []string{"test_vault/e2e.db", "vault.db", "e2e.db", "test_vault/livefire.db", "livefire.db"}
   ```
   While functional during testing, this heuristic is an architectural symptom of missing a formal, co-located workspace state container.
4. **The `please-swift` Precedent**:
   In experimental companion explorations (`please-swift`, [ADR 004](004-swift-apple-ecosystem-evolution.md)), project-level hermeticity was achieved by encapsulating workspace configuration and local conversation state inside a dedicated, git-ignored `.please/` directory.

Terminal-native developer ecosystems—most famously `git` (`.git/`), but also `cargo` (`.cargo/`), `vscode` (`.vscode/`), and `terraform` (`.terraform/`)—rely on a co-located dot-directory to anchor workspace-local state, coupled with a well-known global anchor in the user's home directory (`~/.gitconfig`).

We require a unified configuration and persistence model that delivers complete workspace hermeticity, clear discovery precedence, and an automated setup ceremony.

---

## Decision

We will adopt a **Two-Tier Dot-Directory Architecture** for configuration and persistence, retire the OS-specific `Application Support` directory in favor of a universal `~/.please/` global anchor, align physical storage paths with the `workspace` and `global` scopes of the persistent memory subsystem ([ADR 014](014-persistent-agent-memory-and-cybernetic-recall.md)), and introduce the **`please init`** ceremony.

```
┌────────────────────────────────────────────────────────────────────────┐
│                      Two-Tier Dot-Directory Model                      │
├───────────────────────────────────┬────────────────────────────────────┤
│ 1. Workspace-Local Anchor         │ 2. User-Global Anchor              │
│    <workspace_root>/.please/      │    $HOME/.please/                  │
├───────────────────────────────────┼────────────────────────────────────┤
│ • .please/config.json (Overrides) │ • ~/.please/config.json (Defaults) │
│ • .please/vault.db    (Local DAG) │ • ~/.please/vault.db    (Global)   │
│ • .please/vault.db-wal            │ • Scope: 'global' Memories         │
│ • Scope: 'workspace' Memories     │ • Fallback DAG for non-repo work   │
│ • Automatically Git-Ignored       │ • Universal Unix dot-directory     │
└───────────────────────────────────┴────────────────────────────────────┘
```

---

## Detailed Design & Architecture

### 1. The Two-Tier Storage and Configuration Model

#### A. Workspace-Local Anchor (`<workspace>/.please/`)
When working inside a software repository or project directory:
- **`.please/config.json`**: Optional repository-level configuration overrides (e.g. project-specific local model selection, custom system prompts, disabled unsafe tools, or endpoint overrides).
- **`.please/vault.db`**: The SQLite database encapsulating the project's conversation DAG and `scope: workspace` agent memories. The database is strictly isolated to this project.
- **Git Hygiene**: The `.please/` directory is by definition private operator state and must be git-ignored by default.

#### B. User-Global Anchor (`$HOME/.please/`)
Universal user preferences and fallback storage:
- **`~/.please/config.json`**: Primary user settings (default provider, global API keys, default models, acoustic theatrics, natural pacing). Replaces `~/Library/Application Support/please/config.json`.
- **`~/.please/vault.db`**: Global database storing `scope: global` cross-project developer preferences (e.g., formatting styles, commit conventions, communication habits) and conversation nodes executed outside any initialized workspace.

---

### 2. Resolution Precedence (The Discovery Ladder)

When `please` (CLI, TUI, or daemon) resolves its configuration and vault paths, it executes a strict, deterministic resolution ladder:

```
┌──────────────────────────────────────────────────────────┐
│                   Discovery Ladder                       │
└────────────────────────────┬─────────────────────────────┘
                             │
                             ▼
               [1. Explicit CLI Flags (-c / -v)]
                             │
                      Found? ├── Yes ──► Use explicitly specified paths
                             │ No
                             ▼
              [2. Local Workspace Anchor (.please/)]
         Inspect current working directory (zero crawling)
                             │
                      Found? ├── Yes ──► Use .please/config.json & .please/vault.db
                             │ No
                             ▼
               [3. Global Anchor (~/.please/)]
                             │
                      Found? ├── Yes ──► Use ~/.please/config.json & ~/.please/vault.db
                             │ No
                             ▼
               [4. Compiled Built-In Defaults]
         (Auto-bootstrap ~/.please/ on first interactive run)
```

#### Deterministic Precedence Rules:
1. **CLI Flags Override All**: `-c, --config <path>` and `-v, --vault <path>` always take absolute priority.
2. **Local Workspace Precedence (Zero Climbing)**: If `.please/` exists in the targeted working directory (and is not the global `~/.please` anchor), `.please/` is adopted as the workspace anchor. Please does **not** crawl upwards into parent directories or downwards into child subtrees. If a directory lacks a `.please/` directory, it operates purely in global mode.
3. **Configuration Cascading / Merging**: 
   - Global configuration (`~/.please/config.json`) is loaded as the baseline.
   - Workspace configuration (`.please/config.json`), if present in the working directory, is merged on top as an override layer.
4. **Vault Binding**:
   - In an initialized workspace, `vault.db` is anchored to `.please/vault.db`.
   - Outside an initialized workspace, `vault.db` defaults to `~/.please/vault.db`.

---

### 3. The `please init` Ceremony

Patterned after `git init`, the `please init` command establishes a self-contained environment with zero cognitive friction.

```bash
# Initialize current directory as a Please workspace
please init

# Bootstrap global configuration in ~/.please/
please init --global
```

#### A. Workspace Initialization (`please init`)
When executed within a project directory:
1. **Workspace Root Detection**: Identifies repository root via `.git` or defaults to current working directory.
2. **Directory Scaffolding**: Creates `.please/` (with permissions `0755`).
3. **Configuration Synthesis**: If `.please/config.json` does not exist, writes a starter project configuration inheriting from the global config or defaults.
4. **Database Initialization**: Creates `.please/vault.db` with WAL mode enabled (`PRAGMA journal_mode=WAL`), compiles the pure DAG schema ([ADR 002](002-graph-sqlite-storage.md)), sets up memory tables ([ADR 014](014-persistent-agent-memory-and-cybernetic-recall.md)), and establishes Genesis Node 0.
5. **Git Hygiene Automation**:
   - Checks for the presence of `.git/` in the workspace root.
   - If present, inspects `.gitignore`.
   - If `.please/` (or `.please`) is not already ignored, atomically appends:
     ```gitignore

     # Please agent local state and conversation vault (ADR 016)
     .please/
     ```
   - Emits clean, reassuring terminal feedback to the operator.

#### B. Terminal Presentation Mockup (`please init`)

```
╭──────────────────────────────────────────────────────────────────────────╮
│ 🦉 Initializing Please Workspace                                          │
╰──────────────────────────────────────────────────────────────────────────╯
  ✔ Created workspace directory: /Users/bart/Code/my-app/.please
  ✔ Seeded workspace configuration: .please/config.json
  ✔ Initialized conversation & memory vault: .please/vault.db (WAL mode)
  ✔ Appended '.please/' to .gitignore

🎉 Workspace ready! Run 'please' to start your first session.
```

---

### 4. Memory Subsystem Physical Alignment (ADR 014 Synthesis)

[ADR 014](014-persistent-agent-memory-and-cybernetic-recall.md) introduced `MemoryScope`:
- `ScopeWorkspace`: Invariants and facts specific to a repository.
- `ScopeGlobal`: Habits and preferences that follow the developer everywhere.

Previously, both scopes were stored in whichever single `vault.db` was active. With ADR 016:
- **`ScopeWorkspace` Memories** reside in `<workspace>/.please/vault.db`. When you delete or archive a repository, its workspace memories stay encapsulated with it.
- **`ScopeGlobal` Memories** are read from `~/.please/vault.db`.
- **Federated Engine Recall**: The engine query in `Manager.BuildLLMContext()` seamlessly stitches global preferences from `~/.please/vault.db` together with project invariants from `.please/vault.db`, giving the agent unified recall without cross-repository data contamination.

---

### 5. Migration Strategy & Backwards Compatibility

1. **Automatic Home Migration**:
   - If `~/.please/config.json` does not exist, but `~/Library/Application Support/please/config.json` (or `~/.config/please/config.json`) exists, `LoadConfig()` automatically copies the existing configuration to `~/.please/config.json`.
2. **Legacy `vault.db` in Root**:
   - When `please init` runs in a directory containing an existing root `vault.db`, it prompts or automatically migrates `vault.db`, `vault.db-wal`, and `vault.db-shm` into `.please/`.
3. **Candidate List Deprecation**:
   - The ad-hoc fallback list in `cmd/please/memory.go` (`test_vault/e2e.db`, `vault.db`, `e2e.db`) is replaced by the canonical discovery ladder:
     1. `-v <path>`
     2. `.please/vault.db`
     3. `~/.please/vault.db`
     4. `test_vault/e2e.db` (retained only when running in test/e2e mode).

---

## Consequences

### Positive
- **Intuitive Developer Ergonomics**: Replaces hidden OS application paths with the familiar Unix `~/.please/` and `./.please/` dot-directory patterns.
- **Root Working Tree Cleanliness**: Zero stray `vault.db`, `vault.db-wal`, or `vault.db-shm` files polluting project directories.
- **Automated Git Hygiene**: `please init` automatically protects `.gitignore`, preventing accidental commits of multi-megabyte SQLite databases.
- **Physical Memory Alignment**: 1:1 conceptual mapping between ADR-014 `ScopeWorkspace` / `ScopeGlobal` and `.please/vault.db` / `~/.please/vault.db`.
- **Deterministic Local Discovery**: Eliminates fragile crawling heuristics in favor of a strictly local, non-crawling working tree resolver.

### Negative / Trade-offs
- **New CLI Command**: Adds `please init` to the CLI surface area (though highly natural for any developer familiar with `git init`, `npm init`, or `cargo init`).
- **Migration Surface**: Requires transparent migration code in `internal/config` to ensure existing users on macOS Application Support continue to function without breaking.

---

## References

- [ADR 002: SQLite Storage for Graph Persistence](002-graph-sqlite-storage.md)
- [ADR 004: Swift Port and Apple Ecosystem Evolution](004-swift-apple-ecosystem-evolution.md)
- [ADR 008: Modular Config Extraction](008-modular-config-extraction.md)
- [ADR 009: Modular Storage Extraction](009-modular-storage-extraction.md)
- [ADR 014: Persistent Agent Memory Subsystem, Vault Schema, and Cybernetic Recall](014-persistent-agent-memory-and-cybernetic-recall.md)
- [ADR 015: End-to-End Living Scenarios, Seeded Database Fixtures, and Test Subsystem Repatriation](015-living-scenarios-and-test-subsystem-decomposition.md)
