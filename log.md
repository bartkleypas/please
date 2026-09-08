---
type: Concept
title: "Change Log"
description: "All updates and modifications to this knowledge bundle are tracked chronologically below."
tags:
  - please
  - go
  - log
timestamp: "2026-07-09T14:38:00-07:00"
---

# Change Log

All updates and modifications to this knowledge bundle are tracked chronologically below.

## 2026-07-05

*   **Initialized OKF Bundle**: Created the project-level [index.md](index.md) and [log.md](log.md).
*   **Structured Decisions Index**: Created the [decisions/index.md](decisions/index.md) sub-index and initialized ADRs [001-tui-framework.md](decisions/001-tui-framework.md) (Bubble Tea/Lipgloss), [002-graph-sqlite-storage.md](decisions/002-graph-sqlite-storage.md) (SQLite WAL mode), and [003-embedded-visualizer.md](decisions/003-embedded-visualizer.md) (Embedded D3 server).
*   **Documented Core Concepts**: Added [context_resonance.md](docs/context_resonance.md) describing the exponential decay pruning algorithm, and [natural_pacing.md](docs/natural_pacing.md) detailing the punctuation-sensitive stream streaming loop.
*   **Documented Engine Package Structure**: Created the sub-index at [internal/engine/index.md](internal/engine/index.md) describing the core Go packages for the DAG graph model, database persistence, LLM client drivers, and JIT tools.
*   **Documented TUI Package Structure**: Created the sub-index at [internal/tui/index.md](internal/tui/index.md) mapping Bubble Tea model states, TUI event handlers, visual view modules, keymaps, and streaming components.

## 2026-07-06

*   **Refactored Automated Context Supplement**: Adjusted `generateSystemSupplement()` in [internal/tui/handlers.go](internal/tui/handlers.go) to read from `index.md` instead of `README.md` for folder-level index knowledge.

## 2026-07-08

*   **Fixed CLI and TUI Harness Bugs**:
    *   Resolved validation crash on CLI piped stdin by auto-generating tool IDs for `RoleTool` node creation in [service.go](internal/engine/service.go).
    *   Enabled combining piped stdin context and positional arguments sequentially in [main.go](cmd/please/main.go).
    *   Fixed TUI `ctrl+c` exit failure in `/map` mode by hoisting the exit check to the top of `handleKeyEvent` in [keys.go](internal/tui/keys.go).
    *   Stopped hidden text input keystroke capture when viewport overrides are active in [keys.go](internal/tui/keys.go).

## 2026-07-09

*   **Approved ssh Tool**: Added `ssh` to the `allowedCommands` list in [tool_defaults.go](internal/engine/tool_defaults.go) to allow the harness engine to use SSH commands.
*   **Streamlined Agent Bootstrap Memory**: Refactored [GEMINI.md](GEMINI.md) into a thin orientation anchor, deleting redundant features, configurations, and resolved debates, and pointing agents to authoritative source files while preserving developer reminders.

## 2026-08-20

*   **Model Runner Parameters**: Added `ModelOptions` struct supporting `temperature`, `top_p`, `top_k`, `num_ctx`, and `max_tokens` with request payload mappings for Ollama and OpenAI providers in [config.go](internal/engine/config.go), [llm.go](internal/engine/llm.go), [openai.go](internal/engine/openai.go), and CLI flags in [main.go](cmd/please/main.go).
*   **OpenAI Reasoning Token Streaming**: Added parsing for `reasoning_content` and `reasoning` deltas in [openai.go](internal/engine/openai.go) to stream thinking tokens over OpenAI-compatible endpoints (DeepSeek-R1, Ollama `/v1`) to `thoughtChan`.
*   **Automated Test Isolation**: Implemented `GetConfigDir()` respecting `PLEASE_CONFIG_DIR` in [config.go](internal/engine/config.go) and isolated all unit test suites (`t.Setenv("PLEASE_CONFIG_DIR", tmpDir)`), protecting user configs from test writes.
*   **Configurable Workspace Directory**: Added `workspace_dir` setting to `Config` with `~` and `$HOME` environment variable expansion in [config.go](internal/engine/config.go), scoped tool sandboxing and command execution in [tool_defaults.go](internal/engine/tool_defaults.go), scoped persona context generation in [handlers.go](internal/tui/handlers.go), and added the `-w`/`--workspace` CLI flag.
*   **Encryption Key & Redacted Display**: Added `/config key` management in [commands.go](internal/tui/commands.go) and ensured the configuration sheet masks encryption secrets (`•••••••• (configured)`).

## 2026-08-21

*   **"Rewind & Edit" User Turn Navigation**: Implemented `navigateToNode()` in [chat.go](internal/tui/chat.go) utilized by `/jump` in [commands.go](internal/tui/commands.go) and Enter in [keys.go](internal/tui/keys.go) (`ModeMap`). Navigating to an existing user turn rewinds `CurrentID` to the preceding parent turn, clears succeeding chat history from the viewport, and pre-populates `TextInput` (and attached images) with the user turn's content for rapid branch editing. Assistant/system turn navigation retains direct jumping with an empty prompt box.
*   **Server-Sent Events (SSE) Streaming API**: Implemented real-time streaming endpoint `POST /api/v1/chat/stream` in [stream.go](internal/server/stream.go) emitting typed `thought`, `token`, `tool_call`, `tool_result`, and `node_complete` events with iterative host tool execution loops up to configurable depth.
*   **REST API v1 Suite & Bearer Auth**: Implemented complete DAG and node CRUD handlers (`/api/v1/graph`, `/api/v1/nodes`, `/api/v1/branches/{id}`, `/api/v1/supernodes`, `/api/v1/gc`, `/api/v1/tools`, `/api/v1/health`), CORS, and Bearer token auth middleware in [server.go](internal/server/server.go).
*   **20-Year Internal PKI Certificate Generator**: Added `Generate20YearCerts` in [cert.go](internal/server/cert.go) to generate self-signed ECDSA Root CA and Server Leaf certificates valid for 7,300 days with SANs (`localhost`, `127.0.0.1`, `please.local`), eliminating SSL cert expiration friction for local and home network setups.
*   **Engine Daemon & Cert CLI Subcommands**: Added `please serve` and `please cert generate` subcommands in [main.go](cmd/please/main.go) with flags for port, host, TLS, auto-certificate generation, and authentication tokens.
*   **Schema Version Migration (v1 ➔ v2)**: Replaced fragile manual sync loops with a clean schema migration in [config.go](internal/engine/config.go) that auto-upgrades legacy flat configs on disk to namespaced `ServerConfig` and `ClientConfig` blocks on first load.
*   **Remote Daemon SSE Provider**: Implemented `RemoteDaemonProvider` in [remote.go](internal/engine/remote.go) conforming to `LLMProvider`, consuming the `/api/v1/chat/stream` SSE protocol and demuxing thinking tokens, content chunks, and host tool calls over HTTP/TLS.
*   **Conversational CLI Ergonomics (`please connect`)**: Added `please connect [url]` subcommand in [main.go](cmd/please/main.go) to launch the TUI in connected client mode with dynamic `[Connected: <url> 🟢]` title bar badges in [view.go](internal/tui/view.go), while preserving bare `please` as the default standalone TUI.
*   **Segmented TUI Configuration Sheet & `/config remote`**: Updated `/config` in [commands.go](internal/tui/commands.go) to display a clean segmented view showing Active Session mode (`Standalone` vs `Connected`), `[ Server / Engine Backend ]`, and `[ Client / TUI Preferences ]`, along with `/config remote <url>` to adjust remote daemon endpoints on the fly.
*   **Remote Daemon Storage Layer (`RemoteDaemonStorage`)**: Implemented `RemoteDaemonStorage` in [remote.go](internal/engine/remote.go) and enhanced `POST /api/v1/nodes` in [server.go](internal/server/server.go) to proxy node mutations and graph fetches over HTTP, enabling connected TUI clients to persist conversation nodes directly into the daemon's vault without local storage file dependencies.

## 2026-08-23

*   **Turn Deduplication & Node ID Propagation**: Added `node_id` forwarding in `RemoteDaemonProvider.GenerateResponseStream` ([remote.go](internal/engine/remote.go)) and updated `handleChatStream` ([stream.go](internal/server/stream.go)) to reuse the client-generated user node, permanently eliminating ghost duplicate sibling branches.
*   **Flattened Channel Multiplexing**: Refactored SSE stream handling in [stream.go](internal/server/stream.go) to use a symmetrical Go channel `nil`-ing select loop, guaranteeing race-free complete draining of content, thoughts, and tool calls.
*   **Resilient SQLite Timestamp Scanning**: Extracted table-driven `parseFlexibleTimestamp()` in [storage.go](internal/engine/storage.go) supporting RFC3339Nano, standard SQLite timestamps, and sub-millisecond fractional timestamps across database loads and graph reconstruction.
*   **Targeted Reasoning Token Folding**: Added per-node thought folding in [model.go](internal/tui/model.go), [chat.go](internal/tui/chat.go), and [keys.go](internal/tui/keys.go) with `Tab` / `Shift+Tab` / `/fold` keybindings. Renders sleek `▶ Thought Process (N chars) • [Tab to expand]` badges in scrollback while keeping live active thoughts 100% visible, with viewport offset anchoring to prevent scroll jumping.
*   **Live Map Selection & Prune Resilience**: Updated `syncMapSelection()` in [chat.go](internal/tui/chat.go) and [events.go](internal/tui/events.go) to lock onto active `node.ID` rather than array indices, preventing cursor jitter during real-time remote graph growth and adding graceful fallback on remote branch pruning.
*   **Budget-Aware Dynamic Context Retention**: Wires `num_ctx` (e.g. 128k for Gemma 4) into `BuildLLMContext()` in [service.go](internal/engine/service.go). Enforces 100% full fidelity (zero thought stripping, full observations) under 60% capacity, dynamically expands grace turns for moderate load, and engages protective decay only when crossing 85% capacity.
*   **Emoji Turn Signatures ("Signats") & Persona Visual Identity**: Added `ExtractSignat()` in [signat.go](internal/engine/signat.go) and integrated signats (e.g. `🦉📚`, `🛠️💻`, `🧠📐`) across [service.go](internal/engine/service.go), [handlers.go](internal/tui/handlers.go), `/map` ASCII DAG tree visualization ([map.go](internal/tui/map.go)), and chat role headers ([chat.go](internal/tui/chat.go)).

## 2026-08-31

*   **Write Tool Telemetry & Overwrite Semantics**: Enriched write and edit tools (`write_file`, `edit_file`, `patch_file`, `search_and_replace`) in [tool_defaults.go](internal/engine/tool_defaults.go) with concrete telemetry return strings (path, bytes, lines, action), added explicit `overwrite: bool` support to `write_file`, and updated tool execution guidelines in [handlers.go](internal/tui/handlers.go).
*   **Daemon Wire Protocol Specification**: Authored authoritative client-agnostic protocol specification in [daemon_protocol_spec.md](docs/daemon_protocol_spec.md) covering REST v1 endpoints, Server-Sent Events (SSE) streaming lifecycle, 20-year PKI TLS handshake, and strict RFC 3339 timestamp normalization. Recorded workspace-level [ADR 002](../decisions/002-please-daemon-wire-protocol.md).

## 2026-09-01

*   **Extended Multi-Turn Tool Runway**: Elevated the default multi-turn tool depth ceiling from 10 to 50 in [stream.go](internal/server/stream.go) and added configurable `max_tool_depth` in [config.go](internal/engine/config.go) via `ServerConfig.GetMaxToolDepth()`.
*   **Ephemeral Tool Observation Compaction**: Tuned `BuildLLMContext()` in [service.go](internal/engine/service.go) to eagerly compact intermediate tool observations exceeding 1,000 bytes at `distance >= 2` into concise digests, preventing context clobbering over deep multi-turn sessions.
*   **Model Autonomy & Scratchpad Steering**: Added execution guidelines to `generateSystemSupplement()` in [handlers.go](internal/tui/handlers.go) instructing the model on its extended turn runway and advising it to record essential discoveries in its intermediate thoughts.

## 2026-09-02

*   **Dedicated Append Tool**: Added `append_file` tool in [tool_defaults.go](internal/engine/tool_defaults.go) with automatic line-boundary separation, directory creation, duplicate newline suppression, and rich telemetry.
*   **Pruned Redundant Mutation Tools & Enhanced `edit_file`**: Removed duplicate tools `patch_file` and `search_and_replace` from [tool_defaults.go](internal/engine/tool_defaults.go). Enhanced `edit_file` to default to `replace_string` when `mode` is omitted with `search_block`/`replace_block` alias compatibility, establishing it as the single canonical in-place file mutation tool.
*   **Deterministic Tool Ordering & KV Cache Stabilization**: Replaced non-deterministic Go map iteration in `ToolRegistry.GetTools()` ([tool.go](internal/engine/tool.go)) with a deterministic family sort (Sensory/Read $\rightarrow$ Mutate/Write $\rightarrow$ Execute). Eliminates prompt prefix jitter across turns, guaranteeing 100% Ollama KV cache reuse.
*   **ToolCategory Taxonomy & Sandbox Policy Filtering**: Formalized `ToolCategory` (`CategorySensory`, `CategoryMutate`, `CategoryExecute`) on `Tool`, tagged all default tools, and implemented `GetToolsForPolicy()` in [tool.go](internal/engine/tool.go). Under `SandboxPolicyStrict`, execution tools (`execute_command`) are omitted from model context, while maintaining host shell binary whitelists in `sandbox.go`.

## 2026-09-03

*   **Pure Root Persona & Active Perception Architecture ([ADR 003](../decisions/003-ephemeral-leaf-telemetry-pure-root-persona.md))**: Decoupled transient workspace environmental data from the Genesis DAG node. Root nodes (`RoleSystem`) in SQLite persist 100% pure persona, keeping Node 0 pristine. Following empirical live testing with George (where synthetic leaf bumpering caused intent pollution, recitation bias, and cognitive freeze during cross-directory file reading), leaf bumpering was cleanly removed from `BuildLLMContext()` in [service.go](internal/engine/service.go). Human turns remain 100% sacred and un-bumpered, while the agent relies on its deterministic `CategorySensory` tools (`list_directory`, `read_file`, `grep_search`) for active, on-demand perception.
*   **User Configuration for Signat Features & Layered Genesis Prompt**: Added `signat_steering` boolean to `ServerConfig` (defaulting to `false`) and runtime `/config signats on|off` command. When disabled, models operate in a distraction-free mode (zero emoji instructions, emojis omitted from prompt context), while the engine silently derives semantic signats (`deriveSilentSignat()`) from `ToolCategory` (`CategorySensory` $\rightarrow$ `🔍📜`, `CategoryMutate` $\rightarrow$ `🛠️💻`, `CategoryExecute` $\rightarrow$ `🧪⚡`, `Thought` $\rightarrow$ `🧠📐`, `Dialogue` $\rightarrow$ `💬💭`) for 100% TUI and companion Subway Map coverage. When enabled, a layered Genesis prompt overlay (`SignatSteeringContract`) attaches dynamically to `messages[0]` during `BuildLLMContext()`, preserving pure SQLite root storage and KV prefix cache invariants while allowing optional narrative roleplay.

## 2026-09-04

*   **Pruned `inspect_image` & Excised Stable Diffusion Metadata Chain**: Retired the `inspect_image` tool from [tool_defaults.go](internal/engine/tool_defaults.go) and [tool.go](internal/engine/tool.go), removing an esoteric, non-visual tool from the agent's turn 0 system prompt. Completely deleted the legacy Stable Diffusion PNG chunk parser ([metadata.go](internal/engine/metadata.go), [metadata_test.go](internal/engine/metadata_test.go)), the `-inspect-image` flag in [main.go](cmd/please/main.go), the `/parameters` and `/info` slash commands in [commands.go](internal/tui/commands.go), and SD prompt text injections in [service.go](internal/engine/service.go) and [chat.go](internal/tui/chat.go).
*   **Preserved Clean Multimodal Vision & Enhanced Live-Fire Testing**: Retained full multimodal image attachment support (`node.Images`, `/image <path>`, `AttachImages()`, and raw image payload transmission to vision-capable providers). Enhanced the live-fire test suite in [llm_test.go](internal/engine/llm_test.go) with a clean `simulateImageTurn()` routine that prompts George to reflect on his portrait in [Lore/George_image.png](Lore/George_image.png) as Lore-Warden of the Scriptorium without polluting test logs with base64 data.

## 2026-09-05

*   **Structured Peripheral Telemetry & Attentional De-weighting ([ADR 003](../decisions/003-ephemeral-leaf-telemetry-pure-root-persona.md))**: Implemented the synthesized peripheral telemetry architecture. Added [internal/engine/telemetry.go](internal/engine/telemetry.go) to derive bounded, low-entropy environmental telemetry (`cwd`, `git_branch` with 200ms timeout, `local_time`, workspace orientation `index_file`, and optional client editor telemetry `active_file`/`cursor_line`) formatted into isolated XML envelopes (`<USER_REQUEST>` and `<ADDITIONAL_METADATA>`). Layered an invariant `AmbientTelemetryContract` dynamically onto `messages[0]` in `BuildLLMContext()`, instructing the model to treat metadata as passive peripheral context without recitation bias. Projected the envelope strictly onto the active leaf user turn while keeping historical turns and SQLite conversation logs 100% sacred and un-bumpered. Added `ambient_telemetry` to `ServerConfig`, runtime `/config telemetry on|off` command in TUI, wire protocol ingestion in [stream.go](internal/server/stream.go), and comprehensive unit tests.
*   **Modular Tools Subsystem & Sandbox Extraction ([ADR 005](decisions/005-modular-tools-extraction.md))**: Extracted and decomposed the host tool execution runtime, sandbox validation, and default tools from the 765-line `tool_defaults.go` monolith in [internal/engine](internal/engine/) into a dedicated, decoupled [internal/tools](internal/tools/) package with discrete domain files (`registry.go`, `sandbox.go`, `fs.go`, `search.go`, `exec.go`, `defaults.go`). Shrank the `internal/engine` tool footprint from 1,499 lines down to 175 lines via zero-breaking Go type aliases and convenience delegation on `Manager.ExecuteToolCall` and `Manager.RegisterDefaultTools`. Comprehensive test suites verified 100% green across all packages.

## 2026-09-06

*   **Alternate Screen Buffer TUI Lifecycle**: Enabled `tea.WithAltScreen()` for both standalone and connected client TUI sessions in [main.go](cmd/please/main.go). Restores the terminal scrollback buffer cleanly upon exit without leaving frozen TUI chrome or box drawing characters behind.
*   **Custom Configuration File Flag (`-c, --config`) & Hermetic Isolation**: Reassigned the `-c` flag from the redundant TUI shorthand to `-config <path>` across standalone, `serve`, and `connect` commands in [main.go](cmd/please/main.go) (preserving `--chat` for backwards compatibility). Implemented `LoadConfigFile` in [config.go](internal/engine/config.go) with automatic schema migration for flat test configuration files (mapping `sandbox_policy`, `signat_steering`, and `ambient_telemetry`), enabling 100% hermetic isolation when pairing custom configs (`-c ./livefire.json`) with isolated database vaults (`-v ./test_vault/livefire.db`).

## 2026-09-07

*   **Modular LLM Providers Subsystem Extraction ([ADR 006](decisions/006-modular-providers-extraction.md))**: Extracted all LLM provider drivers (`OllamaProvider`, `OpenAIProvider`, `RemoteDaemonProvider`, `MockLLMProvider`), wire serialization protocols, and core message contracts from `internal/engine` into a dedicated [internal/providers](internal/providers/) package (`provider.go`, `options.go`, `ollama.go`, `openai.go`, `remote.go`, `mock.go`). Decoupled `RemoteDaemonStorage` into [storage_remote.go](internal/engine/storage_remote.go) to maintain strict separation between storage and provider runtimes. Replaced the 1,350-line provider footprint in `internal/engine` with zero-breaking Go type aliases (`LLMProvider`, `Message`, `Role`, `ToolCall`, `ModelOptions`), achieving a strict acyclic dependency hierarchy (`tools` $\rightarrow$ `providers` $\rightarrow$ `engine` $\rightarrow$ `server`/`tui`) with 100% test passage across all packages.
*   **Documentation Relocation to `docs/`**: Moved [context_resonance.md](docs/context_resonance.md) and [natural_pacing.md](docs/natural_pacing.md) from the root workspace directory into `docs/`, updating navigation links in [index.md](index.md), [GEMINI.md](GEMINI.md), and autonomous test primers in [llm_test.go](internal/engine/llm_test.go).
*   **Endpoint URL Normalization**: Implemented `NormalizeOllamaEndpoint` and `NormalizeOpenAIEndpoint` in [internal/providers](internal/providers/) and wired them into `NewOllamaProvider`, `NewOpenAIProvider`, and the TUI `/config endpoint` handler. Automatically handles missing URL schemes, trailing slashes, bare hostnames, and base paths (`/v1` $\rightarrow$ `/v1/chat/completions`, host $\rightarrow$ `/api/chat`), removing routing papercuts for local runners and custom proxies.
*   **Multi-Session Daemon Concurrency Architecture ([ADR 007](decisions/007-multi-session-daemon-branch-concurrency.md))**: Documented the architectural analysis of multi-client concurrency against `please serve`, identifying critical failure modes in TUI cursor hijacking (`EventNodeSaved`), compaction temporal rug-pulls across branched lineages, and workspace filesystem contention during tool execution. Proposed a phased roadmap for session head namespacing, branch-scoped compaction, and worktree sandboxing.
*   **Multi-Session Stabilization & Camera Decoupling (ADR 007 Phase 1)**: Decoupled TUI camera control from remote `EventNodeSaved` broadcasts in [internal/tui/events.go](internal/tui/events.go), preventing active client viewports from being yanked when other sessions advance their branches. Introduced explicit `SessionID` generation and `X-Please-Session-ID` header transmission across `RemoteDaemonProvider` ([internal/providers/remote.go](internal/providers/remote.go)), `RemoteDaemonStorage` ([internal/engine/storage_remote.go](internal/engine/storage_remote.go)), and the TUI SSE event stream. Added protection against system root pruning in `PruneBranch` ([internal/engine/service.go](internal/engine/service.go)).
*   **Modular Subsystems Roadmap ([ADR 008](decisions/008-modular-config-extraction.md), [ADR 009](decisions/009-modular-storage-extraction.md), [ADR 010](decisions/010-pure-dag-graph-extraction.md))**: Established the architectural blueprint for the final stages of the `v0.1.x` code redistribution runway leading to `v0.2.0`. Outlined the modular extraction of configuration models (`internal/config`), persistence and vault encryption (`internal/storage`), and the pure in-memory conversation DAG (`internal/graph`), completing a strictly acyclic dependency graph across Please.

## 2026-09-08

*   **Modular Configuration Subsystem Extraction ([ADR 008](decisions/008-modular-config-extraction.md))**: Extracted application configuration schemas (`Config`, `ServerConfig`, `ClientConfig`), directory resolution (`GetConfigDir`), schema migrations (v1 $\rightarrow$ v2), and options parsing from the 411-line `internal/engine/config.go` into a dedicated, leaf [internal/config](internal/config/) package. Replaced the `internal/engine` implementation with clean, zero-breaking Go type aliases and forwarded constructors (`NewDefaultConfig`, `LoadConfig`, `LoadConfigFile`, `GetConfigDir`).
*   **Boolean Preference Naming Alignment (`EnableNaturalPacing`)**: Aligned client streaming pacing method naming with server preference conventions (`EnableSignatSteering`, `EnableAmbientTelemetry`), introducing `EnableNaturalPacing()` on `ClientConfig` and `Config` while retaining `IsPacingEnabled()` as a 100% backward-compatible alias. Updated all TUI call sites in [streaming.go](internal/tui/streaming.go) and [commands.go](internal/tui/commands.go).
