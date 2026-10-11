# 🦉 The Please Application

`Please` is a lightweight, high-performance conversation harness and multi-session agent runtime designed for fast, dynamic interaction with Large Language Models (LLMs).

Unlike linear chat applications, `Please` treats conversations as a **Directed Acyclic Graph (DAG)**, allowing you to branch reality, jump between timelines, delegate isolated subagents, and maintain a persistent, encrypted history across standalone Terminal UI (`please`), headless daemon (`please serve`), connected remote TUI (`please connect`), and modern IDE Agent Client Protocol (`please acp`) surfaces.

---

## ✨ Features

### 🏗️ Engine & Memory Architecture (`v0.3.1`)
* **DAG-Based Navigation & Named Sessions:** Branch, prune, and jump across multiple conversation timelines (`main`, `experiment`, `sub_<id>`) with session-guarded lineage protection.
* **Canonical `SessionHarness` Execution ([ADR 019](decisions/019-lifecycle-convergence-and-canonical-harness-standardization.md)):** Unifies streaming thoughts, tokens, multi-step tool loops, and interactive human consent gates (`PermissionGate`) across standalone TUI, headless daemon (`SessionActor`), and IDE ACP.
* **Cache-Stable Context Shaping ([ADR 020](decisions/020-cache-stable-context-shaping-and-pure-prompt-projections.md)):** Pure read-only `ContextShaper` projections (`sigmoid` default, `window`, `exponential`) driven strictly by topological step distance ($\Delta d$) and token fill ratio—zero wall-clock time decay, preserving **Monotonic Prefix Invariance** for instant KV-cache reuse. See [Cache-Stable Context Shaping & Smart Receipt Paging](docs/context_resonance.md).
* **Three-Tier Memory & Smart Receipts ([ADR 014](decisions/014-persistent-agent-memory-and-cybernetic-recall.md), [ADR 021](decisions/021-tiered-sensory-storage-and-observation-compaction.md)):**
  * **L1 (Working Context Window):** Ephemeral in-flight prompt buffer shaped by `ContextShaper`.
  * **L2 (Sensory Observation Vault):** Out-of-band gzipped telemetry in SQLite `observation_blobs` paired with deterministic in-DAG `SmartReceipt` (`obs_<hash>`) and on-demand virtual paging via `inspect_receipt`.
  * **L3 (Cybernetic Semantic Store):** Durable FTS5-indexed long-term knowledge in SQLite `memories` managed via `memory_store`, `memory_recall`, `memory_delete`, and `memory_diagnose`.
* **Native Delegated Multi-Agent Topologies ([ADR 022](decisions/022-native-delegated-multi-agent-topologies.md)):** First-class sub-session delegation (`spawn_subagent`, `inspect_subagent`, `reconcile_subagent`) with isolated out-of-tree Git worktrees, `target_dir` nested repository support, and synthesized perception shielding.
* **Encrypted SQLite WAL Vault ([ADR 002](decisions/002-graph-sqlite-storage.md), [ADR 009](decisions/009-modular-storage-extraction.md)):** Concurrent SQLite (WAL mode) persistence with optional AES-256-GCM zero-knowledge field encryption (`enc:v1:...`).

### 🎭 Interactive Surfaces & Protocols
* **Hierarchical ViewStack TUI ([ADR 017](decisions/017-tui-viewstack-and-hierarchical-focus-architecture.md)):** Built with [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea) and Lipgloss, routing focus and rendering through composable layers (`ChatLayer`, `MapLayer`, `MemoriesLayer`, `TextLayer`, `ConfirmOverlay`).
* **Visual DAG Mapping (`/map`) & Supernodes (`/compact`):** Visualize branching timelines with Vim-style navigation (`h/j/k/l`), emoji turn signatures ("Signats"), branch pruning (`d`), and Supernode range compaction (`c`).
* **IDE Agent Client Protocol (`please acp` / `please-acp` — [ADR 012](decisions/012-agent-client-protocol-support.md)):** Stdio JSON-RPC 2.0 agent server for Zed, JetBrains, and Apple Xcode (including an automatic Xcode preamble quarantine shim).
* **Multi-Session Engine Daemon (`please serve` — [ADR 007](decisions/007-multi-session-daemon-branch-concurrency.md)):** Headless HTTP/TLS service featuring Server-Sent Events (SSE) turn streaming (`POST /api/v1/chat/stream`), complete DAG REST v1 API, per-session `SessionActor` queues, and 20-year internal PKI certificate minting (`please cert generate`). See the [Daemon Protocol Specification](docs/daemon_protocol_spec.md).
* **Phonic Bell Staging ([ADR 013](decisions/013-acoustic-theatrics-phonic-staging-and-talon-tap-telemetry.md)) & Natural Pacing:** Sounds a terminal bell (`\a`) strictly when human attention is needed (turn completion or consent gate) after draining the punctuation-sensitive [Natural Reading Pacing](docs/natural_pacing.md) stream buffer.

---

## 🔗 CLI Interaction, Diagnostics & Chaining

`Please` can be used interactively or programmatically to initialize workspaces, chain narratives, and inspect the SQLite vault directly from the terminal.

### Initialize a Hermetic Workspace ([ADR 016](decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md))
```bash
# Scaffold .please/config.json and .please/vault.db in the current workspace
please init

# Or initialize the user-global anchor (~/.please/)
please init --global
```

### Add a Message via Stdin or Positional Arguments
```bash
# Pipe via stdin (infers role as 'tool', generates a response, and outputs the assistant's Node ID)
echo "As the fog cleared, a mysterious figure appeared." | please

# Positional prompt (infers role as 'user', generates a response, and outputs the assistant's Node ID)
please "What is the capital of France?"

# One-shot insertion without LLM generation
please --no-gen "This message is just for the graph history."
```

### Chain Messages Across Branches
```bash
ROOT_ID=$(please --role system "You are a detective in 1920s London.")
MSG_ID=$(please --parent $ROOT_ID "I want to investigate the docks.")
please --jump $MSG_ID
```

### Inspect DAG Nodes, Smart Receipts & Shaper Scorecards
Inspect node metadata, tool calls, `SmartReceipt` badges (`obs_<hash>`), active path token costs, and `ContextShaper` fidelity tiers:
```bash
# Inspect active session head or latest leaf node
please inspect

# Inspect a specific node by UUID or short ID prefix
please inspect 01a08802 -v ./.please/vault.db
```

### Inspect Reconstructed LLM Context
View the exact linear message payload (`[]domain.Message`) produced by `ContextShaper` (including `SmartReceipt` compactions, signat/telemetry overlays, and cybernetic memory primers):
```bash
# View human-readable transcript
please context 01a08802

# Dump exact JSON array of messages for piping to jq
please context 01a08802 --json | jq .
```

### Persistent Agent Memory & Vault Maintenance ([ADR 014](decisions/014-persistent-agent-memory-and-cybernetic-recall.md))
Inspect and manage the agent's L3 cybernetic memory vault and L2 observation blobs:
```bash
# List all persistent memories in the vault
please memory list

# Inspect a specific memory with access telemetry, confidence score, and full Markdown content
please memory inspect workflow:test-caching-bypass

# View vault health, category distributions, top-accessed records, and stale candidates
please memory diagnose

# Prune stale scratchpad memories older than 30 days
please memory prune --older-than 720h

# Decrypt an encrypted SQLite vault or clean orphaned observation blobs
please vault decrypt -v ./.please/vault.db
please vault clean -v ./.please/vault.db
```

---

## 🚀 Getting Started

### Prerequisites
* **Go:** 1.26 or higher.
* **LLM Provider:**
    * A local **Ollama** or **MLX** instance running with your preferred model (default: `gemma4:e4b`).
    * *OR* an **OpenAI-compatible** API key and endpoint (OpenAI, OpenRouter, vLLM, llama.cpp).

### Installation

**Option A: Install via Go (Recommended)**
```bash
go install github.com/bartkleypas/please/cmd/please@latest
```

**Option B: Build from Source**
```bash
git clone https://github.com/bartkleypas/please.git
cd please
make build
./please --version
```
*(Note: `make build` and `make install` also create the `please-acp` multicall symlink for one-click IDE registration).*

### Running the App

Launch the standalone interactive TUI:
```bash
make run
# or with isolated Git worktree sandboxing for a named session:
./please --session feature-x --worktree
```

Start the headless multi-session daemon:
```bash
./please serve --port 8080 --worktree
```

Connect the TUI to a remote `Please` daemon:
```bash
./please connect http://localhost:8080 --session feature-x
```

Start the Agent Client Protocol (ACP) server for Zed, JetBrains, or Xcode:
```bash
./please acp
# or invoke via the symlink:
./please-acp
```

Generate 20-year internal Root CA and Server certificates for TLS:
```bash
./please cert generate
./please serve --port 8443 --tls --generate-certs
```

### Configuration
`Please` resolves configuration using the [ADR 016 Discovery Ladder](decisions/016-workspace-dot-directory-hermeticity-and-init-ceremony.md):
1. Explicit CLI flag (`-c / --config <path>`)
2. Environment override (`PLEASE_CONFIG_DIR`)
3. Workspace-local `.please/config.json` (created via `please init`)
4. User-global `~/.please/config.json` (or `~/.config/please/config.json`)

All configurations use **Schema Version 2** with distinct `server` and `client` blocks. Ready-to-use canon presets are located in [`examples/configs/`](examples/configs/README.md) and can be test-driven directly using `please -c <path>`.

**Local Apple Silicon / MLX Preset (`examples/configs/mlx-gemma4-26b.json`)**
```json
{
  "version": 2,
  "mode": "standalone",
  "server": {
    "provider": "openai",
    "model": "gemma4:26b-mlx",
    "endpoint": "http://localhost:11434",
    "api_key": "ollama",
    "storage_type": "sqlite",
    "vault": "./.please/vault.db",
    "context_shaper": "sigmoid",
    "sandbox_policy": "standard",
    "signat_steering": true,
    "ambient_telemetry": true,
    "options": {
      "temperature": 1.0,
      "top_p": 0.95,
      "top_k": 64,
      "min_p": 0.05,
      "num_ctx": 131072,
      "max_tokens": 16384,
      "repeat_penalty": 1.05,
      "repeat_last_n": 128,
      "frequency_penalty": 0.15
    }
  }
}
```

**Local Ollama Runner (`examples/configs/ollama-local.json`)**
```json
{
  "version": 2,
  "mode": "standalone",
  "server": {
    "provider": "ollama",
    "endpoint": "http://localhost:11434",
    "model": "gemma4:9b",
    "storage_type": "sqlite",
    "vault": "./.please/vault.db",
    "context_shaper": "sigmoid",
    "signat_steering": true,
    "options": {
      "temperature": 0.7,
      "top_p": 0.9,
      "top_k": 40,
      "num_ctx": 16384,
      "max_tokens": 4096
    }
  }
}
```

*See [`examples/configs/README.md`](examples/configs/README.md) for Cloud Gateway (OpenRouter), headless daemon, remote thin-client, and lightweight laptop presets.*

---

## 🤖 Runtime TUI Commands

Inside the interactive TUI, use these slash commands to navigate the DAG and control the runtime:

| Command | Description |
| :--- | :--- |
| `/help` | View the command list and navigation tips in a scrollable overlay. |
| `/map` | Push the interactive DAG tree visualizer onto the `ViewStack`. |
| `/memories` | Browse, search, and prune the persistent L3 cybernetic memory card deck (alias: `/memory`). |
| `/session [list \| switch <name>]` | View active session status, list all session heads, or hot-swap sessions. |
| `/worktree [list \| remove <id>]` | Inspect isolated Git worktree sandboxes or prune completed session worktrees. |
| `/sandbox [strict \| standard \| permissive]` | View or switch the active tool execution security policy. |
| `/compact [directive]` | Summarize active branch history into a compacted `RoleSummary` Supernode. |
| `/fold` | Expand or collapse reasoning `<thought>` accordions in the viewport (or press `Tab`). |
| `/attach <path>` | Attach an image to the next user turn for multimodal vision models (alias: `/image`). |
| `/list` | List all node IDs in the conversation graph. |
| `/jump <id>` | Teleport to a specific node using its ID prefix (rewinds user turns for rapid re-editing). |
| `/mark [id]` / `/unmark <id>` | Place or remove a bookmark on a node. |
| `/persona` | Branch the conversation into a new timeline with a custom system prompt. |
| `/config` | View segmented Server/Client configuration sheet (with redacted encryption secrets). |
| `/config workspace <path\|default>` | Set or reset the active workspace directory root. |
| `/config key <secret\|default>` | Set or clear the AES-256-GCM vault encryption key. |
| `/config remote <url>` | Set the remote daemon endpoint URL. |
| `/config worktree <on\|off>` | Toggle automatic Git worktree isolation for named sessions. |
| `/config signats <on\|off>` | Toggle LLM emoji turn signature steering. |
| `/config telemetry <on\|off>` | Toggle `<ADDITIONAL_METADATA>` peripheral workspace telemetry. |
| `/config temp\|top_p\|top_k\|ctx\|max_tokens <val>` | Adjust model sampling and context window parameters on the fly. |
| `/bell [on \| off]` | Toggle phonic terminal bell (`\a`) on turn completion and consent gates. |
| `/pacing [on \| off]` | Toggle punctuation-sensitive natural reading pacing for streamed replies. |
| `/server [on \| off]` | Control the embedded web visualization server. |
| `/audit` | Toggle audit mode to view internal nodes and extended UUIDs. |
| `/gc` | Garbage collect soft-deleted nodes from the database (alias: `/emptytrash`). |
| `/version` | Display the application version and git commit hash. |
| `/q`, `/quit`, or `/bye` | Gracefully exit the application. |

**Navigation & Focus Tips:**
* **Chat Layer:** Use `↑`/`↓` or `PgUp`/`PgDn` to scroll history. Press `Tab` / `Shift+Tab` to toggle reasoning `<thought>` blocks.
* **Streaming Pacing:** Press `ESC`, `Enter`, or `Space` while tokens are streaming to instantly skip pacing and flush the full response.
* **Map Layer (`/map`):**
    * `j`/`k`: Move selection across nodes.
    * `h`/`l`: Collapse/Expand or Ascend/Descend branches.
    * `g`/`G`: Jump to root/leaf.
    * `/`: Real-time fuzzy search across node content.
    * `c`: Compact/Summarize branch into a Supernode.
    * `d`: Soft-prune/Delete branch (guarded by active session heads).
    * `Enter`: Jump to selected node.
* **Modal Overlays:** Press `ESC` (or `q` in read-only sheets) to pop the top layer off the `ViewStack` and return to chat.

---

## 🧪 Development, Testing & Package Architecture

`Please` is built for fast, deterministic verification using hermetic unit tests and multi-turn **Living Scenarios** ([ADR 015](decisions/015-living-scenarios-and-test-subsystem-decomposition.md)).

### Running Tests
Run the fast, hermetic unit and scenario test suite (~2s):
```bash
make test
# or directly:
go test -count=1 ./...
```

Run the canary & end-to-end Living Scenarios against a live local inference runner (requires a running Ollama instance):
```bash
make test-canary
make test-e2e
```

Format Go code and Markdown knowledge files:
```bash
make format
```

### Four-Tier Package Stratification ([ADR 018](decisions/018-package-stratification-and-domain-decoupling.md))

The codebase enforces a strict downward-only acyclic dependency graph across four tiers:

* **Layer 3 — Presentation, Daemon & Entrypoints**
  * [`cmd/please/`](cmd/please/index.md): CLI entrypoint, `please init`, `please serve`, `please connect`, `please acp`, `please inspect`, `please context`, `please memory`, and `please vault`.
  * [`internal/tui/`](internal/tui/index.md): Bubble Tea `ViewStack`, focus layers (`ChatLayer`, `MapLayer`, `MemoriesLayer`, `TextLayer`, `ConfirmOverlay`), phonic bell staging, and natural token pacing.
  * [`internal/server/`](internal/server/index.md): Headless HTTP/TLS daemon, REST v1 API, SSE streaming pump, per-session `SessionActor` mailboxes, and 20-year PKI generator.
  * [`internal/acp/`](internal/acp/index.md): Agent Client Protocol stdio JSON-RPC server and Xcode preamble quarantine shim.
* **Layer 2 — Core Orchestration & Lifecycle**
  * [`internal/engine/`](internal/engine/index.md): Canonical `SessionHarness`, `LocalHarnessProvider`, `ContextShaper` (`SigmoidShaper`, `WindowShaper`, `ResonanceShaper`), `SmartReceipt` synthesis, subagent delegation (`delegate.go`), and peripheral telemetry.
* **Layer 1 — Infrastructure, Capabilities & Subsystems**
  * [`internal/graph/`](internal/graph/index.md): Pure in-memory thread-safe DAG `Graph` and `Node` structures.
  * [`internal/storage/`](internal/storage/index.md): SQLite (WAL mode) for `nodes`, `sessions`, gzipped `observation_blobs` (L2), and FTS5 `memories` (L3), plus JSONL, remote HTTP proxy, and AES-256-GCM encryption.
  * [`internal/tools/`](internal/tools/index.md): Host tool registry, 3-tier sandbox policy gatekeeper, filesystem/search/exec tools, `inspect_receipt`, `memory_*` tools, and `spawn_subagent` delegation.
  * [`internal/providers/`](internal/providers/index.md): Ollama, OpenAI, Remote Daemon SSE, and Mock LLM drivers.
  * [`internal/config/`](internal/config/index.md): Schema v2 configuration, `.please/` workspace discovery ladder, and migrations.
  * [`internal/worktree/`](internal/worktree/index.md): Out-of-tree ephemeral Git worktree manager for concurrent sessions and delegated subagents.
* **Layer 0 — Core Domain Primitives (Zero Internal Dependencies)**
  * [`internal/domain/`](internal/domain/index.md): Foundational `Message`, `Role`, `ToolCall`, `ToolObservation`, `SmartReceipt`, `ToolDefinition`, and `ModelOptions` contracts.
