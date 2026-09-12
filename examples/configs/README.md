# Canon Configuration Presets

This directory contains canon configuration presets for `please` across different model tiers, inference backends, and deployment topologies.

All presets adhere to the **Schema Version 2** standard introduced in [ADR 008](../../decisions/008-modular-config-extraction.md).

---

## ⚡ Quick Start: Test-Driving a Preset

You can test-drive any preset in hermetic isolation without modifying your personal `config.json`:

```bash
# Launch standalone TUI with the 26B MLX flagship preset
please -c examples/configs/mlx-gemma4-26b.json

# Launch with local Ollama
please -c examples/configs/ollama-local.json

# Start a headless daemon with server preset
please serve -c examples/configs/daemon-server.json

# Connect a client TUI to the daemon
please connect -c examples/configs/client-remote.json
```

To make a preset your default configuration, copy it to your system config location:

- **macOS**: `~/Library/Application Support/please/config.json`
- **Linux / Unix**: `~/.config/please/config.json`

```bash
# Example: Make 26B MLX your default on macOS
cp examples/configs/mlx-gemma4-26b.json "$HOME/Library/Application Support/please/config.json"
```

---

## 🗂️ Available Presets

| Preset | Target Backend | Model | Use Case & Hardware |
| :--- | :--- | :--- | :--- |
| [`mlx-gemma4-26b.json`](mlx-gemma4-26b.json) | Local MLX (OpenAI-compatible) | `gemma4:26b-mlx` | **Flagship Local**: High-parameter, 128k context, high-fidelity sampling (`min_p`, `repeat_penalty`, `signat_steering`). Apple Silicon M-series (36GB+ Unified Memory recommended). |
| [`mlx-gemma4-e4b.json`](mlx-gemma4-e4b.json) | Local MLX (OpenAI-compatible) | `gemma4:e4b-mlx` | **Fast / Mobile Local**: Snappy responses, 32k context, optimized for laptops (MacBook Air / Pro with 16GB–24GB RAM). |
| [`ollama-local.json`](ollama-local.json) | Local Ollama | `gemma4:9b` | **Universal Local Runner**: Zero-config local runner connecting to `localhost:11434`. |
| [`openrouter-cloud.json`](openrouter-cloud.json) | OpenRouter / OpenAI API | `anthropic/claude-3.7-sonnet` | **Cloud Multi-Model**: Route prompts through cloud endpoints with environment secret expansion. |
| [`daemon-server.json`](daemon-server.json) | Headless Daemon | `gemma4:26b-mlx` | **Server Daemon**: Runs `please serve` on port 9190 with bearer token auth and SQLite vault persistence. |
| [`client-remote.json`](client-remote.json) | Thin Client | — | **Remote TUI**: Connects `please connect` to a remote or headless daemon with natural pacing enabled. |

---

## 📐 Schema Version 2 Reference

Configurations use the following top-level anatomy:

```json
{
  "version": 2,
  "mode": "standalone",
  "server": { ... },
  "client": { ... }
}
```

### Top-Level Properties

- **`version`** (`int`): Must be `2`.
- **`mode`** (`string`): Deployment mode:
  - `"standalone"`: Runs both the engine and TUI locally in one process.
  - `"server"`: Runs headless daemon (`please serve`).
  - `"client"`: Runs thin TUI client connected to a daemon (`please connect`).

### Server Block (`"server"`)

Controls provider connections, storage vaults, tool execution policies, and model sampling:

| Field | Type | Description |
| :--- | :--- | :--- |
| `provider` | `string` | Inference backend: `"openai"` (supports MLX, vLLM, LMStudio, OpenRouter) or `"ollama"`. |
| `model` | `string` | Target model identifier (e.g. `gemma4:26b-mlx`, `gpt-4o`). |
| `endpoint` | `string` | HTTP URL for the inference API (e.g. `http://localhost:11434` or `https://openrouter.ai/api/v1`). |
| `api_key` | `string` | Authorization key or dummy token (e.g. `"ollama"` or `"local"` for local servers). |
| `storage_type` | `string` | Vault storage engine: `"sqlite"` (recommended) or `"jsonl"`. |
| `vault` | `string` | Custom path to the storage database/vault file. |
| `encryption_key` | `string` | Optional AES-GCM encryption key for zero-knowledge vault storage. |
| `signat_steering` | `bool` | Enables directional signat navigation and contextual guideposts. |
| `ambient_telemetry` | `bool` | Enables live token throughput, latency, and context window metrics in the status bar. |
| `sandbox_policy` | `string` | Tool execution sandbox mode: `"strict"`, `"standard"`, or `"permissive"`. |
| `options` | `object` | Model sampling parameters (see below). |

### Sampling Options (`"options"`)

Fine-grained inference controls passed directly to the LLM backend:

```json
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
```

- **`num_ctx`**: Maximum context window token allocation (e.g. `131072` for 128k).
- **`max_tokens`**: Maximum generation token budget per response.
- **`min_p`**: Minimum probability threshold relative to the most likely token (cleans up tail degeneration).
- **`top_k` / `top_p`**: Nucleus and top-k filtering.
- **`repeat_penalty` / `repeat_last_n`**: Repetition mitigation for long multi-turn DAG branches.

### Client Block (`"client"`)

Configures client connection to a remote daemon:

| Field | Type | Description |
| :--- | :--- | :--- |
| `remote_url` | `string` | URL of the running daemon (e.g. `http://10.0.0.5:9190`). |
| `auth_token` | `string` | Shared secret bearer token required by the daemon. |
| `natural_pacing` | `bool` | Simulates natural human reading cadence for streamed tokens in the TUI. |
