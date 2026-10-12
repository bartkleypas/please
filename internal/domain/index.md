---
type: Concept
title: "Package internal/domain Index"
description: "Zero-dependency core domain models, message contracts, tool schemas, SmartReceipts, and PKI certificate primitives (ADR-018)."
tags:
  - please
  - domain
  - models
  - index
timestamp: "2026-10-11T10:22:00-07:00"
---

# Package internal/domain Index (`internal/domain`)

The `internal/domain` package sits at **Layer 0** of the four-tier architecture ([ADR-018](../../decisions/018-package-stratification-and-domain-decoupling.md)). It defines the shared domain primitives and data contracts used across all upper layers (`storage`, `tools`, `providers`, `engine`, `server`, `tui`, and `acp`).

> [!IMPORTANT]
> **Layer 0 Hermeticity Invariant**: `internal/domain` has **zero** internal imports from `github.com/bartkleypas/please/...` (verified automatically by [hermeticity_test.go](hermeticity_test.go)). It depends exclusively on the Go standard library.

## Core Files & Components

* [models.go](models.go) - Core domain types:
  * **Conversation & Prompt Contracts**: `Role` (`system`, `user`, `assistant`, `tool`, `summary`), `Message`, and `ModelOptions` (`Temperature`, `TopP`, `TopK`, `NumCtx`, `MaxTokens`, penalties).
  * **Tool Execution & Sensory Contracts**: `ToolCategory` (`sensory`, `mutate`, `execute`), `SandboxPolicy` (`strict`, `standard`, `permissive`), `ToolDefinition`, `ToolCall`, and `ToolObservation`.
  * **Smart Receipts & Tiered Telemetry ([ADR-021](../../decisions/021-tiered-sensory-storage-and-observation-compaction.md))**: `SmartReceipt` (`obs_<hash>`), `FormatPromptReceipt()`, `ParseReceiptPrefix()`, and dual-nature `ToolObservation` serialization supporting both structured JSON and legacy read-through.
  * **Domain Errors**: `ErrMemoryNotFound` and shared sentinel errors.
* [certs.go](certs.go) - Shared TLS certificate path resolution (`ResolveCACert`) and PKI constants.
* [domain_test.go](domain_test.go) & [hermeticity_test.go](hermeticity_test.go) - Serialization round-trip tests for `ToolObservation`/`SmartReceipt` and AST import boundary verification enforcing zero internal dependencies.
