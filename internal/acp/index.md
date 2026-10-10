# Agent Client Protocol (ACP)

Implementation of the Agent Client Protocol (ACP), enabling `please` to function as a standardized agent. It provides the necessary bridge between ACP-compliant clients and the `please` runtime.

## Key Components

*   **Agent**: The core ACP agent implementation that manages state and interaction logic.
*   **Server**: The ACP server responsible for handling client connections and routing requests.
*   **Xcode Shim**: A specialized bridge enabling ACP agents to interact with the Xcode environment in a standardized manner.
*   **Session Management**: Logic for handling agent sessions, including the implementation of ACP modes (Strict, Standard, Permissive) to control agent autonomy and constraints.
