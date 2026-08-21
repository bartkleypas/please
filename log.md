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
*   **Documented Core Concepts**: Added [context_resonance.md](context_resonance.md) describing the exponential decay pruning algorithm, and [natural_pacing.md](natural_pacing.md) detailing the punctuation-sensitive stream streaming loop.
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





