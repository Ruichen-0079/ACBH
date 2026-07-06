# ACBH v0.5.1-public-relay-hotfix2

Relay runtime hotfix: sustained tunnel lifecycle, strict `active` semantics, and data-plane forwarding.

## Fixes

- `POST /v1/relay/configure` now starts and maintains heartbeat, lease renew, coordinator keepalive WebSocket, tunnel session consumer, and health-check loops.
- `GET /v1/relay/status` exposes expanded fields (`tunnelConnected`, `sessionPumpRunning`, `lastDisconnectReason`, etc.).
- `active=true` requires all control-plane and data-plane conditions; no more flash-true from a single heartbeat.
- Windows relay client maintains long-lived `→ VPS:6121` WebSocket and pumps player sessions to `127.0.0.1:25565`.
- Coordinator adds `GET /v1/groups/:groupId/relay/clients/host` and richer public-relay ingress logging.
- GUI splits relay health indicators and shows explicit inactive reasons.

## Build

- Commit: `5fae728` (plus version bump commit)
- Windows zip + Linux coordinator tar.gz from `scripts/build-minimal-core-release.ps1`

## Deploy

1. Upload coordinator bundle to VPS, extract, set `ACBH_ACCESS_TOKEN`, restart Coordinator.
2. Extract Windows zip, run `scripts/acbh-minimal-core-gui.ps1`.
3. Confirm Windows zip and coordinator bundle share the same `build-info.json` commit.