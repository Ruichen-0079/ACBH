# ACBH v0.5.1-public-relay-hotfix3

Relay data-plane hotfix: bidirectional byte forwarding only starts after both player and host sides attach.

## Fixes

- `public-relay.ts` waits for coordinator bridge readiness before starting player TCP ↔ tunnel copy; buffers early player bytes.
- `relay.ts` tracks `playerAttached`, `hostAttached`, `bothAttached`, `forwardingStarted`; buffers pre-bridge frames; logs byte counters and close reasons.
- `routes.ts` uses accurate attach event names (`player side attached`, `host side attached`, `host keepalive client attached`).
- Windows `HostRelayClient` reports per-session byte counts and diagnostics via `OnClose`.
- `/v1/relay/status` adds `recentSessions`, `publicMinecraftPingOk`, `recentPublicPingOk`, and real Minecraft status ping health checks.
- GUI shows「公网入口可用」only when public Minecraft status ping succeeds.

## Verify

```text
127.0.0.1:25565 status ping -> JSON OK
<VPS_IP>:25565 status ping -> JSON OK (through relay)
GET /v1/relay/status -> publicMinecraftPingOk=true, recentSessions[].bytesLocalToPlayer > 0
```

## Build

- Windows zip + Linux coordinator tar.gz from `scripts/build-minimal-core-release.ps1`

## Deploy

1. Upload coordinator bundle to VPS, extract, set `ACBH_ACCESS_TOKEN`, restart Coordinator.
2. Extract Windows zip, run `scripts/acbh-minimal-core-gui.ps1`.
3. Confirm Windows zip and coordinator bundle share the same `build-info.json` commit.