---
type: Concept
title: "Package internal/worktree Index"
description: "Out-of-tree ephemeral Git worktree sandboxing for concurrent multi-session agent execution and subagent delegation (ADR-007, ADR-022)."
tags:
  - please
  - worktree
  - git
  - sandboxing
  - index
timestamp: "2026-10-11T10:22:00-07:00"
---

# Package internal/worktree Index (`internal/worktree`)

The `internal/worktree` package (Layer 1: Infrastructure) manages ephemeral, out-of-tree Git worktrees to physically isolate concurrent agent sessions and delegated subagents (`spawn_subagent`) without polluting the user's primary checkout or confusing language servers (`gopls`, `tsserver`), per [ADR-007](../../decisions/007-multi-session-daemon-branch-concurrency.md) and [ADR-022](../../decisions/022-native-delegated-multi-agent-topologies.md).

## Core Files & Components

* [worktree.go](worktree.go) - Defines `Manager` and `WorktreeInfo`. Encapsulates Git repository detection (`IsGitRepo`), collision-resistant repository hashing, worktree provisioning (`EnsureWorktree`, branch `please/<session-id>` or `subsession/<id>`), inspection (`Status`, `DiffStat`), branch reconciliation/merging (`Reconcile`), listing (`List`), and pruning (`Remove`). Supports nested repository delegation via `target_dir` (`v0.3.1`).
* [worktree_test.go](worktree_test.go) - Unit and integration tests verifying branch isolation, out-of-tree mounting, diff inspection, reconciliation, and graceful fallback on non-Git directories.

## Storage Layout

Worktrees are mounted out-of-tree under:
`~/.please/worktrees/<repo-name>-<hash>/<session-id>/`
(or inside the active `PLEASE_CONFIG_DIR` during hermetic test runs).
