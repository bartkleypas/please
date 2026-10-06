# The Layers of the Silent City

*An archival record of the Great Stratification, as transcribed by the Scribe of the Scriptorium.*

In the early days, the Scriptorium was a sprawl—a chaotic expanse of parchment and ink where every scroll whispered to every other, and no boundary held the tide of complexity. It was a city of tangled alleys, where a simple query in the archives might lead one wandering, unplanned, into the depths of the engine-rooms or the heights of the observation decks. This was the Age of the Fuzzy AST, a time of pathological coupling and inverted gazes.

Then came the Decree of Stratification. The city was not to be a flat plain, but a series of ascending strata, each bound by the Law of the Downward Gaze.

## The Bedrock: Layer Zero
At the deepest root lies the **Bedrock**, the `internal/domain`. It is the First Stone, the immutable foundation upon which all other structures lean. The Bedrock is hermetic; it possesses no knowledge of the city that rises above it. It knows only the fundamental truths—the nature of a *Role*, the shape of a *ToolCall*, the essence of an *Observation*. 

The Bedrock does not look up. It is oblivious to the Spires, the Nexus, and the Vaults. It is the silent witness, the zero-dependency truth that anchors the Scriptorium in the void.

## The Vaults: Layer One
Above the Bedrock lie the **Vaults**, the infrastructure and subsystems of the city. Here dwell the specialized guilds: the *Graph-Keepers* who map the conversation nodes, the *Storage-Scribes* who commit ink to the SQLite stones, the *Tool-Smiths* who forge the capabilities of the system, and the *Provider-Seers* who interpret the whispers of the Great Models.

The Vaults may gaze down upon the Bedrock, for they must know the fundamental truths to perform their craft. However, they are forbidden from whispering to one another across the void. A *Provider-Seer* must never seek the counsel of a *Tool-Smith*, nor shall the *Graph-Keepers* depend upon the *Seers*. They operate in focused isolation, ensuring that the machinery of the city remains decoupled and pure.

## The Nexus: Layer Two
Crowning the Vaults is the **Nexus**, the `internal/engine`. This is the great orchestration hub, the heartbeat of the Scriptorium. The Nexus coordinates the turn-loops and manages the lifecycles of the sessions. 

The Nexus is the bridge. It gazes down upon the Vaults and the Bedrock, weaving their disparate capabilities into a coherent symphony of action. It is the sole authority that understands how to evoke a tool and store its result, transforming the raw capabilities of Layer One into the living logic of the system.

## The Spires: Layer Three
At the highest peak are the **Spires**, the presentation surfaces where the Scriptorium meets the world. Here are the *TUI-Glass-Towers*, the *ACP-Gates*, and the *Server-Spires*. 

From the Spires, the view is absolute. They may gaze down upon the Nexus, the Vaults, and the Bedrock, for they must reflect the state of the entire city to the users who dwell beyond the walls. Yet, they are the most fragile of the strata, for they depend upon every layer beneath them.

***

### The Invariants of the Silent City

Let it be known that the laws of the Scriptorium are absolute:

1. **The Law of Downward Gaze**: A layer may only acknowledge the existence of those beneath it. To look upward is to invite chaos; to look sideways is to invite coupling.
2. **The Law of Bedrock Hermeticity**: The First Stone shall remain pure. No import from the city shall ever descend into the Bedrock.
3. **The Law of Provider Isolation**: The Seers of the Providers shall remain ignorant of the tools they invoke, knowing only the schema of the request and the nature of the response.

Thus is the Silent City ordered. Thus is the Scriptorium preserved.
