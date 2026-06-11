# ACBH V1 Release Notes

## Overview

ACBH V1 ("Anyone Can Be Host") is a distributed Minecraft host handoff platform.
It enables one trusted player device to take over hosting from another when the
current host goes offline. V1 uses file-level snapshot synchronization and fast
host takeover. It is not hot migration — players reconnect after takeover.

V1 recovery target: **10–30 seconds**.

## What ACBH V1 can do

### Coordinator

- Create and manage groups with access keys, members, and host registrations.
- Accept periodic heartbeats from candidate host devices.
- Run deterministic host election based on score and freshness.
- Create one-time takeover assignments with secure token verification.
- Persist runtime state to JSON on shutdown; restore on startup.
- Serve WebSocket relay endpoints for host-to-player byte forwarding.
- Manage ephemeral tunnel sessions with generation binding and TTL.

### Agent CLI

- **login** — Join a group, register a host device, save local config.
- **doctor** — Print local diagnostics (OS, arch, CPU, Java, config).
- **heartbeat** — Send one heartbeat with status, artifacts, and connection info.
- **daemon** — Run heartbeat loop with optional auto-takeover.
- **server start/stop/status** — Manage a local Minecraft server process.
- **scan** — Generate manifests for world-snapshots, server-packs, and admin-state.
- **safe-sync** — RCON `save-all flush` then generate a world-snapshot manifest.
- **push/pull** — Upload/download manifest files and object blobs to Coordinator.
- **manifest validate/diff/inspect** — Validate and inspect local manifests.
- **election status/check-timeout** — View host election state.
- **takeover poll/accept/complete/fail/run** — Execute host takeover flow.
- **gc** — Manual artifact garbage collection.
- **relay host** — Connect as host side of a relay tunnel to a local TCP target.
- **relay player** — Listen locally as a player-side relay tunnel proxy.

### Relay / Player proxy

- **Host relay client** (PR28): `acbh-agent relay host` connects to Coordinator
  relay WebSocket and forwards binary traffic to/from a local TCP target.
- **Public Relay MVP** (PR27): Coordinator WebSocket endpoints under
  `/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/{host,player}` with
  host and player authentication.
- **Player local proxy** (PR29): `acbh-agent relay player` listens on a
  configurable local TCP address, accepts a Minecraft client connection, and
  forwards to the Coordinator player relay WebSocket.
- **Relay-only E2E path** (PR29+PR31):
  ```
  Minecraft Client -> Player TCP proxy -> Public Node relay -> Host relay client -> Host Minecraft/Velocity
  ```
  E2E smoke tests verify the full chain with an in-memory relay and TCP echo.

### Local demo

A self-contained relay-only demo is available (PR32):

```bash
cd agent && go run ./cmd/relay-demo
```

It starts an in-memory relay, TCP echo server, Host relay client, Player proxy,
and test client — all with random ports. No real Minecraft server or Coordinator
needed.

### Release packaging

- `scripts/build-agent-release.sh` (PR33) builds static `acbh-agent` and
  `relay-demo` binaries for linux/amd64, linux/arm64, windows/amd64,
  darwin/amd64, and darwin/arm64.
- SHA256 checksums are generated.
- `.github/workflows/agent-release-artifacts.yml` (PR34) runs the build script
  in CI and uploads `dist/` as a workflow artifact.

## Security

- Host tokens, player tokens, takeover tokens, and RCON passwords are never
  logged, stored in plaintext, or exposed in API responses.
- Takeover tokens are stored as SHA256 hashes.
- Artifact publishing is guarded by current-host and generation checks.
- Relay payloads are forwarded as opaque binary frames without inspection.
- Player proxy defaults to loopback (`127.0.0.1`).
- No Minecraft protocol parsing is performed.

## Limitations

The following are intentional V1 limitations:

- **No P2P / direct transport**: All relay traffic goes through the Public Node.
  STUN/ICE signaling, WebRTC, and QUIC are future work.
- **No GUI**: Agent is CLI-only. All Coordinator interaction is via REST API.
- **No auto-update**: Users download new binaries manually.
- **No published GitHub Release**: Artifacts are available as CI workflow
  artifacts only. A permanent GitHub Release page is a future step.
- **No Minecraft protocol parsing**: All relay traffic is treated as opaque bytes.
- **No PostgreSQL**: Coordinator uses in-memory state + JSON file persistence.
- **In-memory relay**: Relay runtime state (pairing, byte counters) is not
  persisted across Coordinator restarts.
- **No resumable transfers**: Interrupted artifact uploads/downloads must be
  retried from the beginning.
- **Coordinator auth**: Uses basic host tokens and one-time access keys.
  OAuth, JWT, or other standard auth frameworks are not yet implemented.

## Suggested next milestones

1. **V1.1 — Release polish**: Publish a real GitHub Release with built binaries
   and release notes. Add `--version` flag to Agent and Coordinator.
2. **V1.2 — P2P / direct transport**: STUN/ICE signaling, direct connection
   probing, WebRTC data channel, automatic relay fallback.
3. **V1.3 — Player auth**: OAuth or access-key-based player authentication for
   relay sessions.
4. **V2 — Coordinator scale**: PostgreSQL persistence, S3-compatible object
   storage, resumable chunked transfers, multi-coordinator deployments.

## Files changed in V1

Key source files:

```
agent/main.go                               # Agent CLI entrypoint
agent/cmd/relay-demo/main.go                # Relay demo entrypoint
agent/internal/cli/*.go                     # CLI commands (relay, takeover, etc.)
agent/internal/relay/*.go                   # Host relay client + E2E tests
agent/internal/playerproxy/*.go             # Player local proxy
agent/internal/agentconfig/*.go             # Agent config
agent/internal/coordinator/*.go             # Coordinator client
agent/internal/takeover/*.go                # Takeover flow
agent/internal/mcserver/*.go                # Local server process manager
agent/internal/manifest/*.go                # Manifest types and operations
agent/internal/scanner/*.go                 # Directory scanner
agent/internal/artifactsync/*.go            # Push/pull sync
agent/internal/rcon/*.go                    # RCON client
apps/coordinator/src/*.ts                   # Coordinator service

scripts/build-agent-release.sh              # Release packaging
.github/workflows/agent-release-artifacts.yml # CI artifact workflow
examples/relay-only-demo/                   # Local demo
```

## See also

- `docs/v1-architecture.md` — V1 architecture overview
- `docs/v1-release-checklist.md` — Release smoke checklist
- `docs/network-design.md` — Network and relay design
- `docs/tunnel-protocol.md` — Tunnel and proxy protocol details
- `docs/security.md` — Security design
- `docs/codex-guide.md` — Implementation guide and PR index
- `docs/release-packaging.md` — Build and release instructions
