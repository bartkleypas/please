---
type: Decision
title: "004: Swift Port Strategy & iPad Mini Device Jump"
description: "Architecture decision record capturing the 30-year evolution of Please, the transition to Swift/iPadOS, and the Intel Mac development bridge strategy."
tags:
  - please
  - swift
  - ipados
  - decision
  - architecture
timestamp: "2026-07-28T12:39:00-05:00"
---

# ADR 004: Swift Port Strategy & iPad Mini Device Jump

## Context

`Please` originated 30 years ago as a humble shell alias—a dependable, friction-free "secret cheat sheet" brought up instantly by typing `please` in the terminal. Over time, it evolved into an LLM-assisted terminal companion featuring a Directed Acyclic Graph (DAG) conversation engine, dynamic Context Resonance scoring, and natural stream pacing.

As device usage patterns shift, the primary interaction shape for `Please` is jumping from purely desktop/terminal workstations to mobile and handheld Apple hardware—specifically, current-generation **iPad Minis** used across family members and daily workflows.

However, the primary development machine is currently an Intel MacBook Pro, which cannot run Apple Silicon GPU/Metal local models (MLX / CoreML) natively. 

## Decision

We will port and evolve `Please` into a modular **Swift Package (`PleasePackage`)** designed for **macOS and iPadOS**, adhering to the following strategic principles:

### 1. The Intel Mac Dev Bridge (`LLMProvider` Protocol)
To allow active development on the Intel MacBook Pro today while targeting iPad Minis:
* Abstract all LLM interaction behind a thread-safe `LLMProvider` protocol.
* **`CloudLLMProvider`**: Uses standard HTTP Server-Sent Events (`URLSession`) to stream from Google Gemini / OpenAI APIs. This enables 100% testable execution on Intel Macs via the Xcode iPadOS Simulator.
* **`LocalAppleLLMProvider`**: Binds to MLX Swift / CoreML / Apple Foundation Models. Automatically unlocks local GPU/Neural Engine inference when compiled and deployed on Apple Silicon (iPad Mini A15/A17 Pro/M-series).

### 2. Multi-Target Package Architecture
The Swift implementation will be structured into decoupled targets:
* **`PleaseEngine`**: Core DAG graph model (`actor DAGStore`), SQLite storage, Context Resonance decay calculation ($V = (W \cdot C) \cdot e^{-k \cdot \Delta t}$), and natural stream pacing.
* **`PleaseUI`**: Adaptive SwiftUI visualizer supporting iPad Mini layouts (Slide Over, Split View, touch/Pencil node navigation, and hardware keyboard shortcuts like `⌘ + Enter`).
* **`PleaseCLI`**: Native macOS CLI binary (`swift-argument-parser`) maintaining 1:1 parity with the classic `please` terminal workflow (`echo ... | please`).

### 3. Zero-Friction Landings
When deployed to a new device (Mac or iPad):
* Boot time must remain under $10\text{ ms}$.
* Default to automatic key retrieval (macOS Keychain) and smart fallback to local offline mode or available API endpoints.

## Consequences

### Positive
* Enables a native, handheld iPad Mini experience with full touch, pencil, and hardware keyboard support.
* Solves the Intel Mac development bottleneck by allowing cloud streaming testing during dev, with seamless local hardware acceleration on deployment.
* Unlocks deep Apple ecosystem features: App Intents, macOS Shortcuts, Menu Bar popovers, and CloudKit graph sync.

### Negative
* Requires maintaining parity between the Go baseline CLI and the new Swift package during the transition phase.

## Status

Accepted (Active Strategic Roadmap).
