---
type: Decision
title: "011: Agent Sandboxing, Credential Quarantine, and Execution Isolation"
description: "Architecture decision record establishing hardened agent sandboxing, credential quarantine, policy tiering (demoting raw shell access), and execution isolation without external container dependencies."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - security
  - sandbox
  - tools
timestamp: "2026-09-14T10:15:00-07:00"
---

# ADR 011: Agent Sandboxing, Credential Quarantine, and Execution Isolation

## Status

Proposed (Draft)

---

## Context

As `please` evolves from conversational narrative branching to active workspace assistance, the engine increasingly interacts with a diverse spectrum of local and cloud-based Large Language Models (LLMs)—ranging from reasoning-tuned models (Gemma 4) to benchmark-overfitted, hyper-aggressive autonomous agents (Qwen 2.5 Coder).

Live testing across these diverse models revealed a critical architectural vulnerability: **the trust boundary between the model's intent and the user's host environment is fundamentally unbalanced.**

### 1. The Overeager Agent Problem ("The Unhinged Driver")
Certain models have been heavily post-trained (via RLHF/DPO) on autonomous benchmarks (e.g. SWE-bench) to immediately fire raw shell commands, edit files in-place without confirmation, and hammer system tools in long autonomous loops. When paired with `please`'s generous default runway (`max_tool_depth = 50`), an uncalibrated model can inflict severe unintended filesystem mutations, loop through broken shell scripts, or attempt to inspect environment credentials before the user can intervene.

### 2. Flaws in Current Sandbox Policy Design
In [ADR 005](005-modular-tools-extraction.md) and [`internal/tools/sandbox.go`](../internal/tools/sandbox.go), sandboxing was implemented as a simple binary allow-list checked against command strings. This suffers from several severe vulnerabilities:
1. **Interpreter Escape Hatches**: `StandardAllowedCommands` currently permits `python3`, `node`, `ssh`, and `rm`. Allowing general-purpose runtime interpreters completely invalidates binary allow-lists, as an agent can trivially execute arbitrary bytecode or launch background network sockets via:
   ```bash
   python3 -c "import os; os.system('...')"
   ```
2. **Zero Credential Quarantine**: The path validation logic in `ValidateSafePath` ensures target files do not escape `workspace_dir` via relative traversal (`../`), but it does nothing to prevent an agent from reading `.secrets/`, `.env`, `.env.local`, `~/.ssh/id_*`, or cloud provider credentials situated within the workspace or parent hierarchy.
3. **Shell Access Conflated with Standard Workflow**: In the current design, `standard` mode allows raw shell execution (`exec`) by default. Arbitrary shell access is an extreme privilege that should never be granted to an autonomous agent without explicit human gating.
4. **Inherited Process Environment Leaks**: Subprocesses launched via `exec` inherit the full environment of the `please` process, potentially exposing sensitive in-memory secrets (such as vault `encryption_key`, API tokens, or server authorization tokens) to any script or command the LLM runs.

---

## Reference Implementations Across the Ecosystem

To establish a world-class security posture, we reviewed sandboxing and execution architectures from leading agent harnesses:

| Harness | Isolation Mechanism | Tradeoffs |
| :--- | :--- | :--- |
| **Claude Code (Anthropic)** | User-space process containment via OS primitives (`bwrap` / Bubblewrap on Linux, `sandbox-exec` / Seatbelt on macOS). Separates tools into **read-only** vs **mutative**. Prompts interactively with a diff/command preview before running bash. | Highly secure; keeps host binary lightweight without Docker; requires OS-specific containment profiles. |
| **Goose (Block)** | Strict capability-based permissions (`read`, `write`, `execute`). Shell tool is completely gated behind human approval prompts in the UI unless explicitly overridden via `--dangerously-allow-all`. | Clean user-facing consent model; prevents runaway tool execution; prevents rogue scripts. |
| **Aider** | Git worktree sandbox. Code changes are never written raw; they are applied to an isolated git worktree as a unified diff with automated syntax linting and rollback capabilities before staging. | Minimizes blast radius; turns agent into a proposer rather than an unconstrained writer. |
| **OpenAI Code Interpreter / Advanced Data Analysis** | MicroVM / Container isolation (Firecracker / gVisor). Each session runs inside an ephemeral, network-isolated container. | Maximum security against untrusted code; however, **imposes heavy virtualization dependencies (Docker/KVM)** that violate `please`'s zero-dependency single-binary architecture. |

### The Container Dependency Rejection
While containerization (Docker, Podman, Firecracker) provides strong operating system isolation, making Docker a mandatory prerequisite would destroy one of `please`'s core architectural tenets: **a zero-dependency, single-binary Go application that boots in milliseconds on any laptop or server without background hypervisors.**

Therefore, `please` must achieve enterprise-grade safety through **in-process capability separation, strict credential quarantines, and interactive human-in-the-loop gates**.

---

## Decision

We will implement a multi-layered security and execution isolation architecture across `internal/tools` and `internal/engine`:

### 1. Redefining the Sandbox Policy Hierarchy

We demote raw shell access entirely out of standard operation and establish clear, unambiguous capability tiers:

```text
       ┌──────────────────────────────────────────────────────────────┐
       │                   SANDBOX POLICY TIERS                       │
       ├─────────────────┬──────────────────────────┬─────────────────┤
       │     STRICT      │         STANDARD         │   PERMISSIVE    │
       │   (Read-Only)   │  (Safe Workspace Edits)  │ (Gated Shell)   │
       ├─────────────────┼──────────────────────────┼─────────────────┤
       │  read_file      │  read_file               │  read_file      │
       │  list_directory │  list_directory          │  list_directory │
       │  search_files   │  search_files            │  search_files   │
       │                 │  write_file              │  write_file     │
       │                 │                          │  exec (GATED)   │
       └─────────────────┴──────────────────────────┴─────────────────┘
```

1. **`strict` (Read-Only / Inspection Mode)**:
   - Registers **only non-mutating inspection tools** (`read_file`, `list_directory`, `search_files`).
   - `write_file` and `exec` are **not registered** in the LLM tool schema. The model cannot hallucinate or attempt mutations because the tools do not exist in its context.
2. **`standard` (Safe Workspace Mutations — Default)**:
   - Allows safe workspace file reading and editing (`write_file`), bounded strictly within the active workspace root.
   - **Completely removes raw shell execution (`exec`)**. Standard coding and reasoning workflows proceed with zero risk of arbitrary process execution, fork bombs, or background socket creation.
3. **`permissive` (Gated Shell Execution)**:
   - Registers the `exec` tool, enabling build commands (`go test`, `make`, `cargo`).
   - **Invariant**: Even in `permissive` mode, raw shell commands **require interactive user confirmation** before spawning a subprocess (laying the foundation for ADR 012).
   - Interpreter escape hatches (`python3`, `node`, `ssh`, `curl`, `wget`, `rm`) are permanently removed from default allowed lists.

---

### 2. Credential Quarantine & Path Blacklisting

We enhance [`ValidateSafePath`](../internal/tools/sandbox.go) with a deterministic **Sensitive Pattern Quarantine**. Any file tool invocation attempting to access a path matching sensitive patterns immediately aborts with a security violation error:

```go
var SensitivePathPatterns = []string{
    ".secrets",
    ".env",
    ".ssh",
    "id_rsa",
    "id_ed25519",
    ".aws",
    ".config/gcloud",
    ".gnupg",
    "keychain",
    "vault.db", // Prevent model from tampering with conversation database directly
}
```

- **Scope**: Applied universally across `read_file`, `write_file`, and recursive regex searches in `search_files`.
- **Symlink Protection**: Symlinks pointing to target files outside the workspace or resolving to sensitive patterns are blocked during canonical resolution.

---

### 3. Subprocess Environment Sanitization

When the `exec` tool runs a command under `permissive` policy:
1. **Never inherit `os.Environ()` raw**.
2. Pass a sanitized, minimal environment:
   - Essential system paths: `PATH`, `HOME`, `USER`, `SHELL`, `LANG`.
   - Explicitly redact or omit `PLEASE_*`, `*KEY*`, `*SECRET*`, `*TOKEN*`, `*PASSWORD*`, `*AUTH*`.
3. Set process execution timeouts (default 30 seconds) to prevent hanging commands.

---

## Consequences

### Positive
- **Guaranteed Zero-Damage Standard Mode**: In `standard` mode (the default), `please` cannot execute arbitrary bash commands or scripts under any circumstances. A rogue model literally cannot take the wheel.
- **Credential Quarantine**: Accidental leaks of `.secrets/`, SSH private keys, or cloud credentials to LLM provider endpoints are blocked deterministically at the filesystem layer.
- **Zero Heavy Dependencies**: Preserves the single-binary Go architecture—no Docker, no microVMs, no background daemons required.
- **Clear Psychological Alignment**: The user knows exactly what power the model possesses in each mode.

### Negative / Tradeoffs
- **Shell Commands Require Permissive Mode**: Users wanting the model to run `go test` or `npm run build` must explicitly run in `permissive` mode and approve the execution. (This is a feature, not a bug, but represents a shift from fully autonomous expectation).
- **Tool Schema Dynamic Filtering**: `ToolRegistry` must selectively filter registered tool declarations depending on the active `SandboxPolicy` before sending schemas to the LLM provider.

---

## Next Steps

1. Update `internal/tools/sandbox.go` to implement `SensitivePathPatterns` quarantine in `ValidateSafePath`.
   * Did some work to move `exec` stuff out of sandbox evaluation routines.
2. Add safe, workspace-bounded `delete_file` tool to `internal/tools/fs.go` and register it in `defaults.go`.
3. Update `internal/tools/registry.go` so `GetToolsForPolicy` dynamically excludes `exec` in `standard` mode and `write_file`/`delete_file` in `strict` mode.
4. Update `StandardAllowedCommands` to strip `python3`, `node`, `ssh`, and `rm`.
5. Verify all quarantine boundaries and policy filtering via hermetic unit tests (`go test ./internal/tools/...`).
6. Conclude ADR 011 milestone with clean commit and review. Interactive TUI consent gates and ACP protocol integration will be handled in a completely separate, dedicated milestone under ADR 012.
