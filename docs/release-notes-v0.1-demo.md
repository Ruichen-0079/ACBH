# ACBH v0.1-demo Release Notes

**Branch**: `release/v0.1-demo-prep`
**Status**: pre-release demonstration — NOT production ready

## What is v0.1-demo

v0.1-demo is the first shippable ACBH snapshot for internal testing and
demonstration. It bundles a stable in-memory Coordinator, a cross-platform Go
Agent CLI, a loopback-only Local Control HTTP API, and a browser-based Dashboard
control center. The full host handoff loop (scan → push → latest → pull →
restore → verify) works on a single machine without a real Minecraft server or
public network.

## Completed capabilities

| Area | Status |
|------|--------|
| Coordinator in-memory API | Group, member, host registration; heartbeat; artifact push/pull; election; takeover; player/tunnel sessions; relay WebSocket |
| Agent CLI | login, doctor, scan, safe-sync, push, pull, relay host, relay player, daemon, takeover |
| Server process manager | structured argv start/stop/status, process lock, supervisor, graceful stop, repair-state |
| Local Control API | /health, /v1/doctor, /v1/scan, /v1/safe-sync, /v1/push, /v1/pull, /v1/server/status, /v1/server/start, /v1/server/stop |
| Dashboard (GUI) | group/coordinator/agent/storage/artifact/election/events tabs; Chinese + English; OS/platform selector |
| Storage | local filesystem, content-addressed sha256 objects, streaming upload, atomic manifests |
| Artifact GC | retention per kind, min-age, dry-run, fail-closed on manifest read errors |
| Coordinator persistence | JSON state file with token hashes only; temp-file + rename |
| Manifest validation | Go + TS shared fixtures; sorted paths, class-artifact compatibility, summary consistency, tombstone markers, path escape rejection |
| Security hardening | constant-time token compare, masked output, 0600 token files, loopback-only CORS, localStorage-free secrets, error messages never leak tokens, fail-closed GC |
| Auth roles | accessKey, hostToken, playerToken, takeoverToken; cross-group + cross-role rejection; expiry ≤ now enforcement |
| Command argv | structured `jvmArgs`/`serverArgs` in Local Control API; legacy `--command` string compat |
| Demo smoke | `scripts/demo-smoke.sh` — build → health → group → host → heartbeat → scan → push → latest → pull → restore verify → group state → cleanup; no real Minecraft, no public network |
| Verify-all | `scripts/verify-all.sh` + `scripts/verify-all.ps1` — go vet, go test, pnpm build, coordinator test |
| CI | Go test + vet + pnpm build + coordinator test on push/PR |

## Security defaults

- Local Control binds to `127.0.0.1:6122`. Remote binding requires
  `--allow-remote-control` and prints a warning.
- All protected endpoints require a bearer token. The token is stored with
  `0600` permissions; the full value is never printed.
- Dashboard secrets (accessKey, hostToken, agentToken, rconPassword) are
  memory-only, cleared on refresh, never stored in localStorage,
  sessionStorage, IndexedDB, URL, or console.
- Demo scripts use temp files for auth headers and JSON bodies — secrets are
  never passed as process arguments.
- `safe-sync` prefers `ACBH_RCON_PASSWORD` over `--rcon-password` flag.
- Login demo uses `ACBH_ACCESS_KEY` environment variable; key is unset
  immediately after use.

## GUI control console

The Dashboard at `http://127.0.0.1:6121/dashboard` provides:

- **Overview**: group, coordinator, storage, latest artifact summaries
- **Coordinator**: create group, view health, configure access key / host token
- **Agent**: connect to Local Control, run doctor/scan/safe-sync/push/pull,
  start/stop/status managed server, generate CLI commands
- **Storage**: backend info, refresh
- **Artifacts**: list, latest, manifest download, pull
- **Election / Takeover**: election status, run election, check timeout, poll,
  accept, complete, fail assignments
- **Events**: page-local operation log with credential redaction

## Recommended deployment path

- Follow the [single VPS dual-stack deployment guide](zh-CN/deploy-single-vps-dual-stack.md)
  for a low-cost public entry with Velocity A/Fabric A and a standby
  Velocity B/Fabric B pair.
- v0.1-demo recommends two public player ports with manual or semi-automatic
  failover. Players reconnect to the standby entry after a fault.
- The Dashboard can assist the artifact push, pull, restore, server start, and
  takeover rehearsal.
- Automatic Velocity backend switching is planned for v0.2 and is not part of
  v0.1-demo.

## CLI demo smoke

```bash
bash scripts/demo-smoke.sh
```

Builds Coordinator + Agent, starts Coordinator on a random loopback port,
creates a group and host, sends a heartbeat, scans and validates a fake
manifest, pushes and pulls an artifact, verifies the restored file, checks
authenticated group state, and cleans up. Requires Go, Node 20+, pnpm 9+,
curl.

## Known limitations

- **No real Minecraft server integration in smoke tests.** The smoke demo uses a
  fake server directory with dummy files.
- **No live JVM migration.** V1 is file-level snapshot sync, not seamless hot
  migration. Players must reconnect.
- **No proxy layer.** The Coordinator does not sit in the Minecraft traffic path.
  Direct connections are not transparent.
- **No auto-GC.** Artifact garbage collection must be triggered manually.
- **In-memory Coordinator.** State must be persisted explicitly via
  `ACBH_COORDINATOR_STATE_PATH`. Restarting the Coordinator without persistence
  loses all state.
- **Local storage backend.** S3 and other backends are not implemented.
- **No HTTPS/WSS in demo.** The Coordinator and Local Control API use plain HTTP
  on loopback. Production deployments should use TLS.
- **Player session creation is unauthenticated.** Anyone who knows a group ID
  can create a player session.
- **Owner role not enforced.** The `owner`/`member` distinction exists in the
  data model but is not used for authorization decisions.
- **No token revocation API.** Tokens are permanent until expiry or manual
  removal from persisted state.

## Not suitable for production

- v0.1-demo is a pre-release snapshot. Do not deploy to the public Internet
  or expose to untrusted networks.
- Do not use with real Minecraft worlds that you cannot afford to lose.
- State persistence to disk is optional; without it, all state is lost on
  restart.
- Plain HTTP on loopback is acceptable for local development only.

## Platform verification

| Platform | Go tests | Coordinator tests | demo-smoke | verify-all |
|----------|----------|-------------------|------------|-----------|
| Fedora 41 | ✅ PASS | ✅ 123/123 | ✅ PASS | ✅ PASS |
| Windows 11 (PowerShell) | ✅ PASS | ✅ 123/123 | N/A (bash) | ✅ PASS (ps1) |

## Next steps after v0.1-demo

- S3-compatible storage backend
- Auto-GC scheduling
- Stable coordinator persistence with migration
- Player session authentication
- TLS support for Coordinator and Local Control API
- Windows batch demo-smoke script
- Multi-machine relay E2E test
- Release artifacts and versioning workflow
