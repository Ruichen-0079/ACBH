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
- GUI shows listener status, process metadata, public endpoint, current-device state, and relay errors through body API calls only.
- No Minecraft lifecycle control is added. Operators still start the server with MCSL or their own script before probing listener status.

## Phase 2.5 Identity Model

v0.5 uses a single-owner/private instance model in user-visible runtime and GUI surfaces. Users should think in terms of one private ACBH instance, one VPS, one current device, one Minecraft server directory, and one access token.

`config.json` schemaVersion 2 stores user-facing identity in `instance`, `device`, and `server`. The `compat` section keeps Coordinator protocol v2 fields for existing VPS compatibility:

- `compat.legacyGroupId`
- `compat.legacyMemberId`
- `compat.legacyHostId`
- `compat.legacyHostToken`

Runtime code maps schemaVersion 2 identity to Coordinator v2 group routes through an identity adapter. Business modules should not read legacy group fields directly.

## Phase 3 Backup And Snapshot

Phase 3 adds the minimal backup/snapshot main path behind the body API:

- analyze the configured Minecraft server directory
- upload changed backup objects to the VPS
- commit a snapshot manifest
- list snapshots
- download latest or selected snapshots into an explicit restore target directory

The GUI remains a thin body API client. It shows a `备份 / 快照` panel and does not call the VPS directly. Backup/snapshot work does not require the local Minecraft listener to be running.

Downloads never overwrite `server.dir` by default. The caller must pass `targetDir`; existing non-empty targets are blocked unless `allowNonEmpty=true`. Restore path validation treats top-level files such as `banned-ips.json` as valid snapshot entries while still blocking path traversal, symlinks, and Windows reparse points/junctions.

## Frozen Paths

The old server supervisor, `server.lock` repair, GUI Java/bat launch, takeover/election UI, and local Coordinator fallback remain in the tree for compatibility, but they are not on the v0.5 main path. Coordinator protocol v2 still uses legacy group routes internally; protocol v3 can remove that compatibility layer after existing VPS data is migrated.
