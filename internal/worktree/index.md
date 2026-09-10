---
type: Concept
title: "Package internal/worktree Index"
description: "Out-of-tree ephemeral Git worktree sandboxing for concurrent multi-session agent execution (ADR-007 Phase 3)."
tags:
  - please
  - worktree
  - git
  - sandboxing
  - index
timestamp: "2026-09-10T11:42:00-07:00"
---

# Package internal/worktree Index

The `internal/worktree` package manages out-of-tree Git worktrees to isolate concurrent agent sessions without polluting the primary workspace or confusing language servers (`gopls`, `tsserver`), per [ADR-007](../../decisions/007-multi-session-daemon-branch-concurrency.md).

## Core Files & Components

*   [worktree.go](worktree.go) - Defines `Manager`, `WorktreeSession`, directory hashing, branch creation, worktree mounting, and clean pruning.
*   [worktree_test.go](worktree_test.go) - Unit and integration tests testing branch isolation, out-of-tree cleanup, uncommitted changes, and non-git fallback.

## Storage Layout

Worktrees are created out-of-tree under:
`~/.config/please/worktrees/<repo-name>-<hash>/<session-id>/`
(or `$HOME/Library/Application Support/please/worktrees/...` on macOS).
