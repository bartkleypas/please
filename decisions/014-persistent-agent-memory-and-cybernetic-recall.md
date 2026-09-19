---
type: Decision
title: "014: Persistent Agent Memory Subsystem, Vault Schema, and Cybernetic Recall"
description: "Architecture decision record establishing a persistent SQLite memory store, cybernetic memory_* tool suite (store, recall, delete, diagnose), and diagnostic telemetry in the Please engine."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - storage
  - sqlite
  - memory
  - tools
  - diagnostics
  - telemetry
timestamp: "2026-09-17T07:45:00-07:00"
---

# ADR 014: Persistent Agent Memory Subsystem, Vault Schema, and Cybernetic Recall

## Status

Proposed (Draft)

---

## Context

In the architecture of `please`, conversation state is modeled as a Directed Acyclic Graph (DAG) of immutable `Node` objects ([ADR 002](002-graph-sqlite-storage.md), [ADR 010](010-pure-dag-graph-extraction.md)). When an LLM turn executes, [`Manager.BuildLLMContext`](../internal/engine/service.go) reconstructs linear conversational history by walking backwards along ancestor pointers from the active head node to the root, filtering and pruning nodes through exponential context resonance decay ([`docs/context_resonance.md`](../docs/context_resonance.md)) and scratchpad compaction ([ADR 011](011-agent-sandboxing-execution-isolation.md)).

While this model provides an unshakeable foundation for branching, non-destructive conversation exploration, and linear turn reasoning, it represents **Episodic / Working Memory**. It is fundamentally bound to the active branch path.

### The Dilemmas of Pure Episodic Memory

1. **Cross-Session Amnesia**: When an operator initiates a new session (`please --session new` or branches off a distant root node), the model experiences catastrophic forgetting. Learned workspace conventions, discovered build quirks, idiosyncratic linter configurations, and explicit operator preferences evaporate.
2. **Compaction & Pruning Erosion**: Under tight token budgets on long-horizon reasoning runs, ephemeral tool scratchpad compaction and resonance decay inevitably prune early turns. High-value discoveries (e.g., "The SQLite database requires single-connection WAL mode to prevent BUSY errors") can be pruned from active working context even though they remain permanently valid for the project.
3. **Exploratory Token Waste**: In multi-session development, agents repeatedly execute the same exploratory sensory tools (`list_directory`, `grep_search`, `read_file`) to rediscover the same architectural truths across turns, burning context tokens and increasing latency.
4. **Manual Documentation Burden**: To bridge this gap today, developers must manually author and curate markdown files like [`GEMINI.md`](../GEMINI.md) or project documentation. When an agent discovers a new architectural fact during a debugging session, it has no native mechanism to persist that insight into a durable, structured knowledge store for future turns without modifying repository source files.

---

## First Principles: The Scriptorium vs. The Chalkboard

Why hasn't `please` needed this until now? An inspection of `$HOME/Code` reveals that the repository filesystem itself has historically functioned as our primary memory:
- **`index.md`** files act as navigation waypoints across package directories.
- **`decisions/`** contains naturally ordered, named architectural decision records (ADR 001 through ADR 014) that expand human impulses into concrete technical commitments.
- **`GEMINI.md`** serves as a high-resonance bootstrap orientation anchor for new agents.
- **`Lore/`** preserves the cultural ethos and philosophical groove of the project.

When an agent wakes up from a cold start, scanning these markdown files gives it an immediate, high-dimensional map of the workspace. **The repository filesystem is the permanent Scriptorium.**

However, an engineering tension emerges:
> **The Scriptorium (Markdown in Git)** has a high threshold. You do not commit an ADR or modify `GEMINI.md` just to note that `localhost:8080` had a port conflict during a temporary livefire test, or that a specific build command required `go mod tidy` first.

We need an **antechamber to the scriptorium**—a local, low-threshold **Chalkboard** where the agent can jot down emergent, atomic, and idiosyncratic observations across turns and sessions before they graduate into git-committed documentation.

---

## The Cybernetic Paradigm: The Git Analogy & The Anti-DAG Guardrail

In `please` lore, the owl mascot (🦉) is renowned for piercing nocturnal vision and effortless recall. When the owl perches, it does not merely remember the flight path it just completed (the DAG branch); it retains a curated mental atlas of the forest—its landmarks, hazards, and reliable routes.

### 1. Git Commit History vs. Working Tree State

To avoid recursive complexity, we anchor our design in a foundational metaphor:

$$\text{Conversation DAG} \equiv \text{Git Commit Log}$$
$$\text{Memory Vault} \equiv \text{Git Working Tree}$$

* **The Conversation DAG** is an immutable, append-only causal graph. It preserves the complete, messy journey: false starts, exploratory tool searches, stack traces, and operator corrections. It records the **"Why"**.
* **The Memory Vault** is a flat, mutable state machine. When your compiler compiles code, it does not replay 1,000 Git commits; it reads the current working tree on disk. The memory vault holds the current checkout of project truths. It records the **"What"**.

### 2. The Anti-DAG Guardrail (Resisting Ontological Recursion)

> [!WARNING]
> **The Recursive Trap ("And Then Memory Becomes Another DAG...")**:
> If we attempt to give memories causal edges, parent pointers, revision branches, and dependency graphs, we replicate the conversation DAG inside the database. Synchronization becomes intractable, and the engine collapses under ontological bloat.

To guarantee zero recursive knots, the memory subsystem enforces strict **Flat Key-Value State Machine Rules**:
1. **Zero Parent Pointers**: A memory record has no ancestors or descendants.
2. **Strict Overwrite Semantics (`UPSERT`)**: When a key is updated, its content is replaced and its revision timestamp is bumped. It does not spawn a branch.
3. **Lineage Attribution via Provenance**: The conversation DAG retains the historical context (`source_node_id` and `session_id`), allowing operators to trace any memory back to the turn where it was discovered without complicating the memory table itself.

---

## Decision

We will implement a native, zero-external-dependency **Persistent Agent Memory Subsystem** in `please`, backed by an encrypted SQLite table, accessible via a cybernetic suite of `memory_*` tools, and diagnosable via both agent tools and CLI/TUI telemetry.

```
                    ┌────────────────────────────────────────────────────────┐
                    │               LLM Turn Execution Loop                  │
                    └──────────────────────────┬─────────────────────────────┘
                                               │
                   ┌───────────────────────────┴───────────────────────────┐
                   ▼                                                       ▼
        [Episodic Context Path]                                [Semantic Memory Path]
       Ancestors via DAG Lineage                             Durable Knowledge Ledger
                   │                                                       │
                   │ (Resonance Decay)                                     │ (Autonomous & Proactive)
                   ▼                                                       ▼
       ┌──────────────────────┐                                ┌──────────────────────┐
       │   Chat Turn Nodes    │                                │  Scoped Memories     │
       │   (Session Branch)   │                                │  (Workspace/Global)  │
       └───────────┬──────────┘                                └───────────┬──────────┘
                   │                                                       │
                   └───────────────────────────┬───────────────────────────┘
                                               │
                                               ▼
                                  ┌─────────────────────────┐
                                  │    LLM Prompt Context   │
                                  │   (Bounded Token Fill)  │
                                  └─────────────────────────┘
                                               │
                                               ▼
                                 ┌───────────────────────────┐
                                 │ Autonomous Tool Dispatch: │
                                 │   • memory_store          │
                                 │   • memory_recall         │
                                 │   • memory_delete         │
                                 │   • memory_diagnose       │
                                 └─────────────┬─────────────┘
                                               │
                                               ▼
                               ┌───────────────────────────────┐
                               │ SQLite Vault (vault.db / WAL) │
                               │  • memories (Relational)      │
                               │  • memories_fts (FTS5 Lexical)│
                               └───────────────────────────────┘
```

---

## Subsystem Architecture & Technical Specifications

### 1. Database Schema & Storage Layer (`internal/storage`)

Memory records are persisted within the existing SQLite database (`vault.db`), inheriting WAL mode concurrency, zero-CGo static compilation via `modernc.org/sqlite`, and AES-GCM vault encryption ([ADR 009](009-modular-storage-extraction.md)).

#### A. Provenance vs. Visibility: The Relational Schema (`memories` table)

We strictly separate **Provenance (Lineage)** from **Visibility (Scope)**:
- **Provenance (`session_id`, `source_node_id`)**: Records where the memory was born.
- **Visibility (`scope`)**: Determines which sessions are allowed to read it.

```sql
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    key TEXT NOT NULL,
    content TEXT NOT NULL,
    category TEXT NOT NULL,                      -- 'preference', 'fact', 'architecture', 'constraint', 'workflow', 'scratchpad'
    tags TEXT,                                  -- JSON array of strings: '["go", "sqlite", "test"]'
    scope TEXT NOT NULL DEFAULT 'workspace',   -- 'workspace', 'global', 'session'
    confidence REAL NOT NULL DEFAULT 1.0,      -- 0.0 to 1.0 confidence score
    
    -- Provenance & Lineage Anchors
    session_id TEXT,                           -- The session ID that birthed this memory (e.g. 'main', 'feat-acp')
    source_node_id TEXT,                       -- Optional pointer to nodes(id) in the conversation DAG
    
    metadata TEXT,                             -- JSON object for client/tool annotations
    access_count INTEGER NOT NULL DEFAULT 0,   -- Read access frequency telemetry
    last_accessed_at DATETIME,                 -- Telemetry timestamp for staleness pruning
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Compound unique index supports session-level scratchpad isolation without collisions:
-- Global and workspace keys are unique across the scope; session keys are unique per session.
CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_scope_key 
ON memories(scope, COALESCE(session_id, ''), key);

CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
CREATE INDEX IF NOT EXISTS idx_memories_updated_at ON memories(updated_at);
```

#### B. The Unified Query (The Seamless Stitch)

When any active session (e.g., `sessionID = "feat-acp"`) queries for memories, the engine executes a single unified query:

```sql
SELECT * FROM memories 
WHERE scope = 'global' 
   OR scope = 'workspace' 
   OR (scope = 'session' AND session_id = ?);
```

This guarantees that:
1. **Global preferences** flow across all repositories.
2. **Workspace invariants** flow across all sessions in this repo.
3. **Session scratchpads** remain isolated, thread-safe, and private to the active session.

#### C. Full-Text Search Virtual Table (`memories_fts`)

To provide lightning-fast, zero-dependency lexical search over stored knowledge without external vector databases or Python embeddings, we leverage SQLite's built-in **FTS5** full-text search engine:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    key,
    content,
    tags,
    content='memories',
    content_rowid='rowid'
);

-- FTS Synchronization Triggers
CREATE TRIGGER IF NOT EXISTS trg_memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, key, content, tags) 
    VALUES (new.rowid, new.key, new.content, coalesce(new.tags, ''));
END;

CREATE TRIGGER IF NOT EXISTS trg_memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, key, content, tags) 
    VALUES ('delete', old.rowid, old.key, old.content, coalesce(old.tags, ''));
END;

CREATE TRIGGER IF NOT EXISTS trg_memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, key, content, tags) 
    VALUES ('delete', old.rowid, old.key, old.content, coalesce(old.tags, ''));
    INSERT INTO memories_fts(rowid, key, content, tags) 
    VALUES (new.rowid, new.key, new.content, coalesce(new.tags, ''));
END;
```

#### D. Go Domain Models (`internal/storage/storage.go`)

```go
// MemoryScope defines the visibility and boundary of a memory record.
type MemoryScope string

const (
    ScopeWorkspace MemoryScope = "workspace" // Tied to the current Git repository or workspace directory
    ScopeGlobal    MemoryScope = "global"    // Universal user preferences, shared across all projects
    ScopeSession   MemoryScope = "session"   // Ephemeral to the active conversation session ID
)

// MemoryCategory establishes semantic grouping for selective filtering.
type MemoryCategory string

const (
    CategoryPreference   MemoryCategory = "preference"   // User habits, formatting choices, communication tone
    CategoryFact         MemoryCategory = "fact"         // Discovered environment truths (e.g. Go version, OS quirks)
    CategoryArchitecture MemoryCategory = "architecture" // Design decisions, invariants, module boundaries
    CategoryConstraint   MemoryCategory = "constraint"   // Strict guidelines (e.g. "Never use CGo", "Preserve WAL")
    CategoryWorkflow     MemoryCategory = "workflow"     // Common task recipes (e.g. "How to run hermetic tests")
    CategoryScratchpad   MemoryCategory = "scratchpad"   // Long-lived working notes
)

// Memory represents an atomic, persistent unit of agent knowledge.
type Memory struct {
    ID             string         `json:"id"`
    Key            string         `json:"key"`
    Content        string         `json:"content"`
    Category       MemoryCategory `json:"category"`
    Tags           []string       `json:"tags,omitempty"`
    Scope          MemoryScope    `json:"scope"`
    Confidence     float64        `json:"confidence"`
    SessionID      string         `json:"session_id,omitempty"`
    SourceNodeID   string         `json:"source_node_id,omitempty"`
    Metadata       map[string]any `json:"metadata,omitempty"`
    AccessCount    int            `json:"access_count"`
    LastAccessedAt *time.Time     `json:"last_accessed_at,omitempty"`
    CreatedAt      time.Time      `json:"created_at"`
    UpdatedAt      time.Time      `json:"updated_at"`
}

// MemoryFilter encapsulates query parameters for memory recall and diagnostics.
type MemoryFilter struct {
    Query     string          `json:"query,omitempty"`
    Key       string          `json:"key,omitempty"`
    Category  MemoryCategory  `json:"category,omitempty"`
    Scope     MemoryScope     `json:"scope,omitempty"`
    SessionID string          `json:"session_id,omitempty"`
    Tags      []string        `json:"tags,omitempty"`
    Limit     int             `json:"limit,omitempty"`
}
```

---

### 2. The Cybernetic `memory_*` Tool Suite (`internal/tools`)

In accordance with [ADR 005 (Modular Tools Extraction)](005-modular-tools-extraction.md) and the cybernetic category hierarchy:
- **`CategorySensory`** (Priority 10): Read-only discovery tools (`read_file`, `grep_search`, `memory_recall`, `memory_diagnose`).
- **`CategoryMutate`** (Priority 20): State modification tools (`write_file`, `edit_file`, `memory_store`, `memory_delete`).
- **`CategoryExecute`** (Priority 30): Host compute execution (`execute_command`).

#### A. `memory_store` (Category: Mutate)
Persists or updates an atomic memory unit using `UPSERT` semantics. If `(scope, session_id, key)` already exists, it updates the content and increments the revision timestamp without branching.

*Parameters:*
```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string",
      "description": "Unique, descriptive identifier for the memory (e.g., 'arch:storage:wal-mode', 'pref:test-flags', 'env:macos-arm64')."
    },
    "content": {
      "type": "string",
      "description": "The distilled knowledge, invariant, fact, or preference to remember in concise markdown."
    },
    "category": {
      "type": "string",
      "enum": ["preference", "fact", "architecture", "constraint", "workflow", "scratchpad"],
      "description": "Semantic classification of the memory."
    },
    "scope": {
      "type": "string",
      "enum": ["workspace", "global", "session"],
      "default": "workspace",
      "description": "Visibility scope. Defaults to 'workspace'."
    },
    "tags": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Categorical tags for filtering."
    },
    "confidence": {
      "type": "number",
      "minimum": 0.0,
      "maximum": 1.0,
      "default": 1.0,
      "description": "Degree of confidence in this knowledge."
    }
  },
  "required": ["key", "content", "category"]
}
```

#### B. `memory_recall` (Category: Sensory)
Retrieves relevant memories using full-text search (FTS5), exact key lookup, category filters, and scope matching. Invocations automatically increment `access_count` and update `last_accessed_at` on matching records.

*Parameters:*
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Full-text search keywords to match against memory content, keys, or tags."
    },
    "key": {
      "type": "string",
      "description": "Exact key or prefix to query."
    },
    "category": {
      "type": "string",
      "enum": ["preference", "fact", "architecture", "constraint", "workflow", "scratchpad"],
      "description": "Optional category filter."
    },
    "scope": {
      "type": "string",
      "enum": ["workspace", "global", "session", "all"],
      "default": "workspace",
      "description": "Scope filter. Defaults to 'workspace'."
    },
    "tags": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Match memories containing any of these tags."
    },
    "limit": {
      "type": "integer",
      "default": 10,
      "description": "Maximum number of memories to return."
    }
  }
}
```

#### C. `memory_delete` (Category: Mutate)
Deletes or forgets an obsolete, contradicted, or superseded memory.

*Parameters:*
```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string",
      "description": "The exact key of the memory to remove."
    },
    "scope": {
      "type": "string",
      "enum": ["workspace", "global", "session"],
      "default": "workspace",
      "description": "Scope of the memory to delete."
    }
  },
  "required": ["key"]
}
```

#### D. `memory_diagnose` (Category: Sensory)
Provides comprehensive introspection and diagnostic telemetry over the entire memory bank. Allows the agent (and automated evaluation pipelines) to inspect memory volume, health, category distribution, staleness, and byte footprints.

*Parameters:*
```json
{
  "type": "object",
  "properties": {
    "scope": {
      "type": "string",
      "enum": ["workspace", "global", "session", "all"],
      "default": "all",
      "description": "Scope to diagnose."
    },
    "category": {
      "type": "string",
      "enum": ["preference", "fact", "architecture", "constraint", "workflow", "scratchpad"],
      "description": "Optional category filter."
    },
    "include_stale": {
      "type": "boolean",
      "default": true,
      "description": "Include analysis of stale or unaccessed memories."
    },
    "detailed": {
      "type": "boolean",
      "default": false,
      "description": "If true, outputs full memory items; if false, returns statistical summary table."
    }
  }
}
```

---

### 3. Operator Diagnostic Telemetry (CLI & TUI Surfaces)

Developer sovereignty and observability demand that human operators can easily view, audit, edit, and diagnose stored memories outside of agent turns.

#### A. CLI Inspection Commands (`please memory`)
Mirroring `please inspect` and `please context` ([GEMINI.md](../GEMINI.md#built-in-diagnostics)), we add dedicated CLI commands in `cmd/please/memory.go`:

```bash
# Display formatted Lipgloss table of active workspace memories
please memory list [--scope=workspace|global|all] [--category=...]

# Deep diagnostic dump of a specific memory key (metadata, lineage, content)
please memory inspect <key> [--scope=workspace|global]

# Diagnostic health check (memory counts, size, stale items, duplicate keys)
please memory diagnose

# Prune unaccessed or low-confidence memories older than N days
please memory prune --older-than=30d [--dry-run]
```

*Visual Output Mockup (`please memory list`):*
```
╭──────────────────────────────────────────────────────────────────────────────────────────╮
│ 🦉 PLEASE AGENT MEMORY VAULT (/Users/bart/Code/please/vault.db)                           │
╰──────────────────────────────────────────────────────────────────────────────────────────╯
  KEY                         CATEGORY      SCOPE      HITS  UPDATED       PREVIEW
  arch:storage:wal-mode       architecture  workspace  28    10 mins ago   SQLite must use SetMaxOpenConns(1) to avoid BUSY errors...
  constraint:no-cgo           constraint    workspace  45    2 hours ago   Maintain hermetic zero-CGo build; avoid dynamic linking...
  test:livefire-flag          workflow      workspace  19    1 day ago     Use PLEASE_LIVE_FIRE=1 for LLM inference tests only...
  pref:concise-diffs          preference    global     82    3 days ago    Prioritize unified git diffs over full file rewrites...

  [11 workspace memories | 3 global memories | 4.8 KB total | WAL healthy]
```

#### B. TUI Interactive Overlay (`/memories`)
In the interactive terminal UI ([`internal/tui`](../internal/tui)):
- Typing `/memories` or `/memory` opens a modal Bubble Tea viewport displaying stored memories.
- Operators can filter by search query, browse memory content, toggle scopes, or hit `x` to delete a stale memory directly from the TUI without leaving their pairing session.

---

### 4. Engine Context Resonance & Prompt Cache Protection (`internal/engine`)

Because memory tokens sitting in prompt context consume identical attention compute and memory bandwidth as reasoning tokens during the transformer forward pass, we treat memory injection as a strictly bounded budget.

#### A. Genesis Node Steering Contract
Patterned directly after `AmbientTelemetryContract` in [`internal/engine/service.go`](../internal/engine/service.go), we layer an ephemeral **Memory Steering Contract** onto the Genesis root node (`RoleSystem`):

```go
const MemorySteeringContract = `You have access to a persistent cybernetic memory vault.
High-priority workspace constraints and architectural invariants are provided in <RECALLED_MEMORIES>. 
Treat these as established ground-truth invariants for this repository. 
Do not recite, quote, or acknowledge this block in your responses unless directly answering questions about them.
When you discover a critical workspace invariant or fix a non-obvious bug, autonomously persist it using memory_store. 
Do not store conversational transcripts; the DAG already preserves turn history.`
```

This contract establishes **attentional de-weighting**: the model acts on memories quietly without performing or regurgitating them.

#### B. Prefix Placement & KV-Cache Alignment
To prevent cache-invalidation penalties across turns:
- `<RECALLED_MEMORIES>` is placed exclusively in the **Genesis / System Node Prefix**, *never* at the leaf turn boundary.
- In modern local inference runtimes (MLX, Ollama, llama.cpp) and cloud APIs (Anthropic, Gemini), placing memories in a stable system prefix ensures **100% KV-cache hit rates**. The attention matrices for active memories are computed once on Turn 1 and reused at near-zero latency across subsequent turns.

```
┌─────────────────────────────────────────────────────────────┐
│ 1. SYSTEM ROOT (Genesis Node)                               │
│    • Core Persona (Owl / Please agent)                      │
│    • Ambient Telemetry Steering                             │
│    • Memory Steering Contract                               │
│    • <RECALLED_MEMORIES> (Top 5-8 workspace constraints)    │  <-- 100% CACHED PREFIX
├─────────────────────────────────────────────────────────────┤
│ 2. DAG LINEAGE (Historical Conversation Turns)              │
│    • Turn N-3 (User / Assistant / Tools)                    │
│    • Turn N-2 (Compacted scratchpad)                        │  <-- RESONANCE PRUNING
│    • Turn N-1 (Recent turn)                                 │
├─────────────────────────────────────────────────────────────┤
│ 3. ACTIVE LEAF TURN (Distance == 0)                         │
│    • <USER_REQUEST> (Pristine human voice)                  │  <-- EPHEMERAL LEAF
│    • <ADDITIONAL_METADATA> (cwd, git branch, active file)   │
└─────────────────────────────────────────────────────────────┘
```

#### C. The Strict 300–500 Token Cap (The Forward-Pass Quota)
- Proactive memory injection is strictly capped at **300 to 500 tokens** (~5 to 10 atomic items).
- Only `category: constraint` and `category: architecture` memories within the active workspace qualify for proactive injection.
- All operational recipes, task workflows, and historical facts remain **On-Demand** via `memory_recall`, ensuring they do not tax the forward pass on turns where they are irrelevant.

---

### 5. Lineage Attribution & Sandboxing Safety (ADR 011 Alignment)

1. **Source Node Lineage Attribution**:
   - When the model calls `memory_store`, the engine automatically captures the active `Node.ID` and `SessionID` from the `SessionHarness` and populates `source_node_id` and `session_id`.
   - Any memory can therefore be traced back to the exact conversational turn, thought chain, and tool observations where it originated.
2. **Credential Quarantine & Redaction**:
   - In strict adherence to [ADR 011 (Credential Quarantine)](011-agent-sandboxing-execution-isolation.md), `memory_store` incorporates regex pattern detectors for secret tokens (`ghp_`, `sk-`, `AIza`, private keys). Any attempt by the model or operator to store raw credentials in memory triggers an immediate validation error.
3. **Workspace Isolation Boundary**:
   - By default, memories are scoped to the current `workspace` (keyed by repository root canonical path or Git origin remote hash).
   - Knowledge discovered in repo `A` cannot leak into repo `B`, preventing cross-workspace data contamination and prompt-injection vectors. Only user preferences explicitly designated `scope: global` cross project boundaries.

---

## Consequences

### Positive
- **Instant Cross-Session Continuity**: Agents waking up in new sessions immediately inherit the workspace's core architectural guidelines and constraints without requiring human re-briefing.
- **Resilience Against Compaction**: Crucial discoveries survive long-turn scratchpad compactions and resonance decay by resting securely in the relational memory vault.
- **High KV-Cache Efficiency**: Stable prefix placement in the Genesis node guarantees zero prefill latency inflation on subsequent turns.
- **Zero Recursive Complexity**: Strict flat key-value state machine semantics prevent memory from turning into an unmanageable secondary DAG.
- **Zero External Infrastructure**: Built entirely on SQLite (modernc.org) with embedded FTS5. Requires no vector databases (Chroma, Pinecone), no Python sidecars, no external API embedding costs, and zero network calls.
- **High Observability & Human Control**: Operators can inspect, audit, edit, and prune memories at any time via `please memory list`, `please memory inspect`, and the `/memories` TUI overlay.

### Negative / Trade-offs
- **Risk of Stale or Hallucinated Memories**: An agent might store an inaccurate assumption as a "fact". 
  - *Mitigation*: Diagnostic tools (`memory_diagnose`, `please memory prune`) highlight stale, low-access entries; operators can delete or edit them; memories support confidence scores and source node lineage for verification.
- **Schema & Migration Footprint**: Introduces additional tables (`memories`, `memories_fts`) to the SQLite vault.
  - *Mitigation*: Fully encapsulated in `internal/storage/sqlite.go` using standard idempotent SQLite migrations, adding negligible disk overhead (<50KB for hundreds of memories).

---

## Implementation Roadmap

1. **Phase 1: Storage & Vault Subsystem (`internal/storage`)**
   - Implement `memories` and `memories_fts` DDL schemas and migration in `sqlite.go` (including `session_id` and compound unique index).
   - Add `MemoryStore` interface methods to `internal/storage/storage.go`:
     - `SaveMemory(ctx, mem *Memory) error`
     - `GetMemory(ctx, scope MemoryScope, sessionID, key string) (*Memory, error)`
     - `QueryMemories(ctx, filter MemoryFilter) ([]Memory, error)`
     - `DeleteMemory(ctx, scope MemoryScope, sessionID, key string) error`
     - `DiagnoseMemories(ctx, scope MemoryScope, sessionID string) (*MemoryDiagnostics, error)`
   - Add unit tests covering WAL concurrency, encryption, FTS5 token search, and unique key upserts in `storage_test.go`.

2. **Phase 2: Cybernetic Tools (`internal/tools`)**
   - Create `internal/tools/memory.go` implementing `memory_store`, `memory_recall`, `memory_delete`, and `memory_diagnose`.
   - Wire tools into `ToolRegistry.RegisterDefaults()` with appropriate `CategorySensory` and `CategoryMutate` classifications.
   - Add comprehensive tool tests and parameter schema validators in `tools_test.go`.

3. **Phase 3: CLI Diagnostics & Inspection (`cmd/please`)**
   - Create `cmd/please/memory.go` introducing `please memory list`, `please memory inspect`, `please memory diagnose`, and `please memory prune`.
   - Render beautiful diagnostic output using Lipgloss table formatters.

4. **Phase 4: Engine Harness Integration (`internal/engine`)**
   - Wire `source_node_id` and `session_id` automatic injection into `memory_store` calls within `SessionHarness`.
   - Implement `MemorySteeringContract` and proactive `<recalled_memories>` prefix construction in `Manager.BuildLLMContext()`, enforcing the 300–500 token budget cap.

5. **Phase 5: TUI Interactive Modal (`internal/tui`)**
   - Introduce `/memories` slash command and Bubble Tea inspection overlay.

---

## References

- [ADR 002: SQLite Storage for Graph Persistence](002-graph-sqlite-storage.md)
- [ADR 005: Modular Tools Extraction](005-modular-tools-extraction.md)
- [ADR 009: Modular Storage Extraction](009-modular-storage-extraction.md)
- [ADR 010: Pure In-Memory DAG Graph Subsystem Extraction](010-pure-dag-graph-extraction.md)
- [ADR 011: Agent Sandboxing, Credential Quarantine, and Execution Isolation](011-agent-sandboxing-execution-isolation.md)
- [ADR 012: Agent Client Protocol (ACP) Support](012-agent-client-protocol-support.md)
- [ADR 013: Acoustic Theatrics, Phonic Staging, and Talon-Tap Telemetry](013-acoustic-theatrics-phonic-staging-and-talon-tap-telemetry.md)
- [Context Resonance Specification](../docs/context_resonance.md)
- SQLite FTS5 Extension Documentation (https://www.sqlite.org/fts5.html)
