---
type: Decision
title: "013: Acoustic Theatrics, Phonic Staging, and Talon-Tap Telemetry"
description: "Architecture decision record establishing non-blocking acoustic cues, terminal bell staging (\a / ASCII 0x07), and model turn-completion telemetry within the Please TUI."
tags:
  - please
  - go
  - architecture
  - decision
  - adr
  - tui
  - audio
  - telemetry
  - ux
timestamp: "2026-09-16T18:15:00-07:00"
---

# ADR 013: Acoustic Theatrics, Phonic Staging, and Talon-Tap Telemetry

## Status

Proposed (Draft)

---

## Context

As Large Language Models (LLMs) grow increasingly autonomous, agentic, and contemplative, the temporal cadence of user interaction with `please` has fundamentally shifted.

In simple conversational chat, token generation begins within a few hundred milliseconds and concludes in a brief burst. However, in `please`'s modern architecture—featuring deep DAG context assembly, long-horizon chain-of-thought reasoning (`<thought>` streams), and multi-turn autonomous tool execution loops (file edits, sandboxed shell execution, AST searches)—a single model turn can span anywhere from five seconds to upwards of a minute.

During these intervals, developers do not stare unblinkingly at the terminal. They switch tabs to inspect code, context-switch to browser documentation, respond to messages, or look away from their monitors.

Consequently, a critical user experience problem emerges: **How does `please` notify the developer that the model has concluded its turn, relinquished the conversational floor, and is awaiting human guidance?**

---

## The Odyssey: From Overengineering to Ancient Telemetry

Arriving at the solution documented in this record was an engineering odyssey—a tragicomedy of modern systems design where developers repeatedly attempt to reinvent the wheel with high-powered lasers before rediscovering the beauty of the circle.

### Act I: The Siren Song of the Embedded Symphonic Engine
The initial instinct was grand: if `please` is to provide acoustic feedback, it should do so with distinction. We envisioned bespoke audio branding for our owl mascot (🦉)—soft wooden clicks, harmonic chimes, or perhaps the resonant acoustic scrape of owl talons on a redwood perch.

To achieve this, we investigated native audio playback in Go:
1. **Third-Party Audio Libraries (`oto`, `beep`, `malgo`)**: We explored binding cross-platform audio drivers directly into the binary.
2. **Embedded Soundfonts & Waveforms**: Packing raw PCM/WAV buffers into Go binaries via `//go:embed`.
3. **Subprocess Audio Spawning**: Invoking platform-specific CLI players (`afplay` on macOS, `aplay` or `paplay` on Linux, `powershell -c [Console]::Beep()` on Windows).

**The Collision with Reality:**
- **CGo & Linking Pains**: True cross-platform audio libraries invariably introduce CGo dependencies or require dynamic linking against host system libraries (`libasound2-dev` on ALSA, PulseAudio headers, CoreAudio frameworks). This instantly shatters `please`’s core commitment to **hermetic, zero-dependency, static cross-compilation**.
- **Binary Bloat**: Audio decoders and asset buffers inflate the binary footprint by tens of megabytes for what amounts to a transient auditory ping.
- **The Remote Headless Dilemma**: Over SSH, inside Docker containers, within headless cloud devboxes, or across remote tmux sessions, native host audio devices simply do not exist. Audio daemons panic, subprocesses exit with status 1, and the terminal user is met with eerie silence or stderr garbage.

### Act II: The Desktop Notification Quagmire
Next came the operating system notification detour: why not send desktop banners via AppleScript (`osascript`) or Linux DBus (`notify-send`)?

**The Collision with Reality:**
- **Notification Fatigue & Spam**: In active pairing sessions with rapid tool turns, desktop notification centers quickly become a graveyard of dismissable banners.
- **Window Management Chaos**: Banners fail to direct focus back to the specific terminal window among dozens of open virtual desktops.
- **SSH / Remote Isolation**: Desktop notification buses do not survive remote SSH hops without intricate socket tunneling and remote display forwarding.

### Act III: The Epiphany of Ancient Unix Telemetry (ASCII 0x07)
Faced with the wreckage of modern multimedia subsystems, we retreated to first principles: **What primitive has the Unix terminal provided for this exact problem since the era of the Teletype Model 33 in the late 1960s?**

The answer is a single byte:
```
\a  (Hex: 0x07, ASCII: BEL, Control: ^G)
```

The **Terminal Bell** (`BEL`) was physically engineered into electromechanical teleprinters as a solenoid-driven strike against an actual brass bell to alert the human operator that an incoming telegram required immediate attention.

When written to the terminal standard output stream, `\a` delegates the entire acoustics, visual semantics, and user preferences directly to the **terminal emulator** itself:
- In modern emulators (**Ghostty, iTerm2, WezTerm, Kitty, Alacritty, macOS Terminal, Windows Terminal**), the bell is fully configurable by the user:
  - It can ring the native system alert sound at the user's preferred system volume.
  - It can trigger a **Visual Bell** (an elegant full-screen flash or inverted color strobe for silent/accessibility environments).
  - It can bounce the application icon in the macOS Dock or flash the taskbar icon.
  - It can set a subtle notification badge on the terminal tab or tmux window status bar.
- In headless remote environments (**SSH, tmux, mosh**), `\a` is natively propagated across the wire to the client terminal without requiring audio forwarding, X11 displays, or DBus tunnels.

**Result**: Zero external dependencies. Zero CGo. Zero allocations. Zero binary bloat. Total operating system independence. Universal terminal emulator compliance.

---

## The Concepts: Phonic Staging & Talon-Tap Telemetry

While the transmission mechanism (`\a`) is elementary, the *timing, conditions, and cadence* of its emission require rigorous architecture.

### 1. Phonic Staging (The Anti-Ringing State Machine)
An LLM turn is not an indivisible event; it is an orchestrated sequence of discrete phases:
1. **Prompt Ingestion & Context Resonance**: Preparing active DAG history.
2. **Thought Streaming**: Emitting internal reasoning chains (`<thought>...</thought>`).
3. **Paced Content Generation**: Streaming assistant prose through punctuation-aware pacing buffers.
4. **Tool Invocations & Consent Gate**: Emitting tool call payloads, checking sandbox policies, or pausing for human approval.
5. **Tool Execution Loops**: Running shell commands, grepping files, updating ASTs.
6. **Turn Conclusion**: Yielding conversational control back to the operator.

> [!WARNING]
> **The Pavlovian Nightmare**: Emitting a bell on every streaming chunk, intermediate tool call, or pacing tick would produce a deafening, anxiety-inducing cacophony. The bell must be treated as a scarce, deliberate boundary marker.

**Phonic Staging** establishes that acoustic signals may **only** fire upon a strict state transition:
$$\text{State}_{\text{Active}} \longrightarrow \text{State}_{\text{AwaitingUserInput}}$$

Specifically:
- **No Bell on Intermediate Tool Steps**: If the model finishes a thought segment to invoke an autonomous tool (`StandardAllowedCommands`), no bell is emitted; the harness continues working silently.
- **No Bell on Streaming Token Arrival**: Token streaming is visually dynamic but acoustically silent.
- **No Bell on Premature Wire Disconnect**: The bell must respect the **Natural Pacing Buffer**. If the LLM finishes transmitting over HTTP/gRPC in 2 seconds, but the TUI's natural reading pacer is smoothly unwinding text over 6 seconds, the bell must not chime until the final rune has been rendered to the viewport.
- **Bell on Human Attention Needed**:
  1. The model completes its final response and enters the input prompt state.
  2. The model halts at an interactive consent gate (e.g., requesting authorization to run a gated command under strict sandboxing).
  3. The turn encounters an unrecoverable streaming error or rate limit.

### 2. Talon-Tap Telemetry
In the lore of the `please` owl mascot (🦉), this acoustic punctuation is designated **Talon-Tap Telemetry**.

When the owl completes its flight across the DAG, records its observations in SQLite, and alights upon the perch, its talons tap once against the wood—a crisp, unobtrusive signal that the bird is resting, alert, and waiting for the user's next command.

---

## Decision

We will implement non-blocking **Talon-Tap Telemetry** using terminal bell semantics (`\a`) within the `please` TUI and engine event pipelines.

```
┌────────────────────────────────────────────────────────┐
│              LLM Stream / Tool Loop Concludes          │
└──────────────────────────┬─────────────────────────────┘
                           │
                           ▼
              ┌───────────────────────────┐
              │ Is Natural Pacing Active? │
              └────────────┬──────────────┘
                    Yes   │   No
             ┌────────────┘   └─────────────┐
             ▼                              ▼
 ┌───────────────────────┐      ┌───────────────────────┐
 │ Wait for Pacing Buffer│      │ Are More Tool Calls   │
 │ to Drain to Viewport  │      │ Pending Execution?    │
 └───────────┬───────────┘      └───────────┬───────────┘
             │                        Yes   │   No
             └──────────────┬───────────────┘   └───────────┐
                            │ (Keep Working)                │
                            ▼                               ▼
                 [Silent Autonomous Loop]        ┌─────────────────────┐
                                                 │ Check Config:       │
                                                 │ BellOnTurnComplete? │
                                                 └──────────┬──────────┘
                                                      Yes   │   No
                                              ┌─────────────┘   └─────┐
                                              ▼                       ▼
                                   ┌──────────────────────┐      ┌─────────┐
                                   │ Emit \a (ASCII 0x07) │      │ No-op   │
                                   │ (tea.Cmd / os.Stderr)│      └─────────┘
                                   └──────────────────────┘
```

### 1. Architecture & Execution in Bubble Tea
In the Charm Bubble Tea Elm architecture, escape sequences should never be randomly printed into stdout mid-render, as this risks corrupting Lipgloss layouts or alternate screen buffers.

We define a dedicated Bubble Tea command:
```go
// BellCmd emits an ASCII 0x07 (BEL) character to signal turn completion.
func BellCmd() tea.Cmd {
    return func() tea.Msg {
        // Writing to Stderr avoids interfering with formatted stdout pipelines
        // while reliably triggering the terminal emulator's bell handler.
        os.Stderr.Write([]byte("\a"))
        return nil
    }
}
```

### 2. Integration into the Streaming Lifecycle
In [`internal/tui/streaming.go`](../internal/tui/streaming.go), the bell command is batched into the return of `handleLLMStreamFinished` when:
1. `len(msg.toolCalls) == 0` (no autonomous tools left to execute), **AND**
2. `m.PacingActive == false` (or when `handlePacingTick` drains the last rune of `m.PacingBuffer`), **AND**
3. `m.Config.BellOnTurnComplete` is enabled.

If the model halts to request tool confirmation (`m.AwaitingToolConfirmation == true`), the bell also fires immediately to summon the human operator to the consent gate.

### 3. Configuration Schema
In [`internal/config/config.go`](../internal/config/config.go), we introduce a toggle under `ClientConfig`:

```go
type ClientConfig struct {
    RemoteURL          string `json:"remote_url,omitempty"`
    AuthToken          string `json:"auth_token,omitempty"`
    CACertPath         string `json:"ca_cert_path,omitempty"`
    NaturalPacing      *bool  `json:"natural_pacing,omitempty"`
    BellOnTurnComplete *bool  `json:"bell_on_turn_complete,omitempty"` // Defaults to true
    Session            string `json:"session,omitempty"`
}
```

- **Default Setting**: Enabled (`true`).
- **Interactive Command**: A quick TUI slash command `/bell` to toggle acoustic cues on the fly during a session without editing JSON files.
- **Environment Override**: `PLEASE_BELL=0` or `NO_BELL=1` for strict silent terminal scripting.

### 4. Daemon & Remote Telemetry (SSE / Wire Protocol)
For users running `please serve` and connecting via the Swift macOS app, iPadOS client, or web visualizer, terminal `\a` is unavailable on the remote server side.

In the SSE streaming response (`/api/v1/chat/stream`), the daemon emits a formal turn conclusion event:
```json
{
  "event": "turn_complete",
  "data": {
    "node_id": "0191eb5a-...",
    "elapsed_ms": 3420,
    "prompt_tokens": 1240,
    "completion_tokens": 412,
    "telemetry": {
      "signal": "talon_tap"
    }
  }
}
```
Client frontends can catch this event and route it to their native acoustic/haptic systems (e.g., `NSSound.beep()` or `UIFeedbackGenerator` on Apple platforms).

---

## Consequences

### Positive
- **Zero Cognitive Drag**: Developers can safely unfocus the terminal window while long reasoning chains execute, knowing an unobtrusive cue will alert them when the turn concludes.
- **Zero Heavy Dependencies**: No CGo, no dynamic linking, no audio drivers, and zero binary size inflation.
- **Respects User Sovereignty**: Terminal emulators already allow users to tune the bell: users who dislike sound can configure visual flashes or mute it entirely in their terminal preferences, while enthusiasts can assign custom system sounds.
- **Seamless Multiplexing**: Tmux users receive window bell flags (`*`), allowing status bars to indicate when background please sessions finish without audible disruption.

### Negative / Risks
- **Overzealous Terminal Beeping**: On legacy or unconfigured Linux virtual consoles, `\a` might trigger an unpleasantly harsh PC-speaker tone. Providing an immediate `/bell` toggle and honoring `NO_BELL` / `bell_on_turn_complete: false` mitigates this completely.

---

## References

- [ADR 001: Selection of Bubble Tea & Lipgloss TUI Frameworks](001-tui-framework.md)
- [ADR 011: Agent Sandboxing, Credential Quarantine, and Execution Isolation](011-agent-sandboxing-execution-isolation.md)
- [ADR 012: Agent Client Protocol (ACP) Support](012-agent-client-protocol-support.md)
- ECMA-48: Control Functions for Coded Character Sets (7-bit Bell: `0x07`)
