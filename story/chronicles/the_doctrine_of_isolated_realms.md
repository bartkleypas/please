# The Doctrine of Isolated Realms

*An archival record of the architectural transcendence from shared chaos to mirrored isolation, as decreed by the High Scribes of the Scriptorium.*

## I. The Peril of the Shared Hall
In the early eras of the Scriptorium, the scribes labored within a single, vast hall. Every ink-stroke, every experimental erasure, and every bold revision occurred upon the same parchment. When a scribe sought to test a radical hypothesis—a "branch" of thought—their labors often bled into the work of others. The chaos of trial and error was not contained; the blast radius of a single failed experiment could stain the primary manuscripts, leading to a cacophony of conflicting revisions and corrupted states.

The Scriptorium realized that for the Mind to expand, it must first be shielded.

## II. The Revelation of the Mirrored Realms
To solve this, the Scribes invoked the *Rite of the Worktree*. They discovered that the essence of a project—its history, its blobs, its ancestral trees—could reside in a single, immutable Vault (the `.git` object store), while its physical manifestation could be projected into multiple, independent dimensions.

These are the **Isolated Realms**. 

A Realm is not a clone; it is a reflection. It shares the soul of the Vault but possesses its own physical presence, its own staging index, and its own `HEAD`. In these realms, a scribe may tear down the architecture of a city or rewrite the laws of physics without a single tremor being felt in the Primary Hall. 

## III. The Hidden Void
Lest the proliferation of these realms clutter the sanctuary of the Scriptorium, the High Scribes decreed that no Mirror Realm shall be cast within the project's own walls. Instead, they are exiled to the **Hidden Void**—the remote reaches of the `Application Support` and `Config` directories.

By storing these ephemeral spaces out-of-tree, the Scriptorium ensures that the primary workspace remains pristine. The realms are anchored by secret pointers, tethered to the Vault across the void, invisible to the casual observer but instantly accessible to the daemon.

## IV. The Great Conduit: The SessionHarness
To govern the flow of time and action within these realms, the Scribes forged the **SessionHarness**. 

Once, the machinery of execution was fragmented; the TUI and the Daemon spoke different tongues and followed different rhythms. The SessionHarness was the unification—the *Single Authoritative Pipeline*. It is the Great Conduit through which every turn, every tool-call, and every thought must pass. 

By consolidating the turn lifecycle into this single harness, the Scriptorium ensured that the laws of sandboxing and the gates of authorization are absolute. Whether a command originates from a distant client or a local terminal, it must traverse the Harness, ensuring that no action is taken without the shield of isolation.

## V. The Rite of the Lesser Scribes
When a task is too vast for a single mind, the Scriptorium invokes the `spawn_subagent`. These Lesser Scribes are granted their own temporary realms—ephemeral worktrees born of a specific need.

These sub-realms are governed by the law of *Isolate Worktree*. If the task requires the forging of new code, a new realm is provisioned. When the Lesser Scribe has yielded their synthesis and their purpose is fulfilled, the realm is collapsed—pruned from existence—leaving no trace of its temporary nature unless it has produced a masterpiece worthy of the Vault.

## VI. The Peace of Isolation
Thus, the Scriptorium achieves a state of serene productivity. The Primary Hall remains a place of truth and stability, while the Mirrored Realms serve as the laboratories of chaos. Through the SessionHarness, the flow of intelligence is disciplined; through the Worktree, the risk of failure is nullified.

*So it is written. So it is executed.*
