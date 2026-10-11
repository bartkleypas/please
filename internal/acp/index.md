---
type: Concept
title: "Package internal/acp Index"
description: "Agent Client Protocol (ACP) stdio JSON-RPC server, session bridge, interactive permission gates, and Xcode preamble quarantine shim (ADR-012)."
tags:
  - please
  - acp
  - ide
  - xcode
  - index
timestamp: "2026-10-11T10:22:00-07:00"
---

# Package internal/acp Index (`internal/acp`)

The `internal/acp` package (Layer 3: Presentation & Entrypoints) implements the **Agent Client Protocol (ACP)** per [ADR-012](../../decisions/012-agent-client-protocol-support.md), enabling `Please` to operate as a first-class external coding agent inside modern IDEs (Zed, JetBrains, Apple Xcode).

## Core Files & Components

* [agent.go](agent.go) - Implements `PleaseAgent` conforming to the 11-method `acp.Agent` contract (`Initialize`, `NewSession`, `ListSessions`, `ResumeSession`, `Cancel`, `CloseSession`, `SetSessionMode`, `SetSessionConfigOption`, `Authenticate`, `Logout`, `Prompt`). Bridges ACP sessions and streaming events (`thought`, `token`, `tool_call`, `tool_result`) directly to `engine.SessionHarness` and routes interactive tool approvals through `conn.RequestPermission`.
* [server.go](server.go) - Configures the stdio JSON-RPC 2.0 transport stream, enforcing strict `os.Stdout` protocol framing while redirecting all internal logs to `os.Stderr`.
* [xcode_shim.go](xcode_shim.go) - Implements the Xcode IDE Quarantine Shim (`SanitizeXcodePrompt`). Strips injected Xcode system preambles, phantom tool directives (`XcodeRead`, `XcodeWrite`), and `<system-reminder>` noise before persistence, while extracting active file, cursor line, and selected code snippets into clean `<ADDITIONAL_METADATA>` ambient telemetry ([ADR-003](../../decisions/003-ephemeral-leaf-telemetry-pure-root-persona.md)).
* [agent_test.go](agent_test.go) & [xcode_shim_test.go](xcode_shim_test.go) - Hermetic unit and in-memory stdio pipe integration tests covering capability handshakes, session genesis isolation, permission gates, and Xcode prompt sanitization.
