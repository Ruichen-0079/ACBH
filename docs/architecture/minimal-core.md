# ACBH v0.5 Minimal Core

v0.5 minimal core splits the product into a local body/runtime, a thin desktop GUI, and the remote Coordinator.

## Stage 1 Scope

- `acbh-agent body serve` listens on `127.0.0.1:6120`.
- Desktop GUI calls only the body API.
- The authoritative local config is `%APPDATA%/ACBH/config.json`.
- Remote public mode never falls back to `127.0.0.1:6121`.
- Coordinator probe reports the actual URL, HTTP status, and response body.

## Phase 2 Scope

- Body detects whether the configured local Minecraft TCP listener exists.
- Listener detection reports process metadata when the OS allows it, and returns a warning instead of failing when process inspection is limited.
- Body configures coordinator relay state by ensuring the active host lease and sending a heartbeat with the local Minecraft endpoint.
- GUI shows listener status, process metadata, public endpoint, current-host state, and relay errors through body API calls only.
- No Minecraft lifecycle control is added. Operators still start the server with MCSL or their own script before probing listener status.

## Frozen Paths

The old server supervisor, `server.lock` repair, GUI Java/bat launch, takeover/election UI, backup/snapshot flows, and local Coordinator fallback remain in the tree for compatibility, but they are not on the v0.5 main path.
