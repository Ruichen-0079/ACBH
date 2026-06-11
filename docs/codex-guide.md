# Codex Implementation Guide

Read this before implementing ACBH tasks.

## Project rule

Do not implement hot migration.

ACBH V1 is file-level snapshot sync plus fast host takeover.

## First implementation target

Create a runnable repository skeleton:

- Coordinator starts and returns `/health`.
- Agent CLI starts and returns help.
- `doctor` command prints basic local checks.
- CI runs TypeScript build and Go tests.

## Preferred order

1. Keep docs accurate.
2. Bootstrap Coordinator.
3. Bootstrap Agent CLI.
4. Add local storage interface.
5. Add manifest generation.
6. Add RCON safe sync.
7. Add heartbeat and election.
8. Build Host A to Host B demo.

## Constraints

- Do not introduce a proxy layer in V1 core handoff loop. Player tunnel/proxy architecture groundwork is in PR26.
- Do not require Minecraft mods.
- Do not parse chunk data.
- Do not upload files directly from fsnotify events.
- Do not store secrets in plaintext.
- Do not let stale hosts overwrite the latest snapshot (artifact publish is guarded by current-host + generation check; SHA256 object blobs remain open for standby pre-warming).
- Do not mix server-pack changes with world snapshots.
- Do not auto-accept local Host changes to mods, plugins, config, or launch metadata.
- Do not treat all server files as one snapshot.
- Do not add a snapshot upload API while working on Agent-local manifest generation.
- Do not include `ignored` or `unknown` files in generated manifests.
- Do not mix artifact kinds in one manifest; scan `world-snapshot`, `server-pack`, and `admin-state` separately.
- Do not implement RCON while working on manifest/object push-pull.
- Do not start or stop Minecraft while working on manifest/object push-pull.
- Do not add host election while working on manifest/object push-pull.
- Do not use JSON/base64 transfer for real Minecraft region files; use the streaming object path.
- Do not add RCON, Minecraft process control, or host election while hardening streaming transfer.
- RCON safe sync may flush and scan, but must not start, stop, or supervise Minecraft.
- Do not add host election while implementing RCON safe sync.
- Never store or print the RCON password.
- Keep the first Minecraft process manager local to the Agent.
- Keep process state and logs local to the Agent user config directory.
- Do not implement host election or automatic takeover while adding local process control.
- Do not add a GUI, proxy, relay, or Coordinator-side Minecraft process control in the process-manager PR.
- Keep election deterministic and expose candidate reasons for debugging.
- Election must create an assignment; it must not start a server or finalize `currentHostId`.
- Only takeover completion may increment `currentHostGeneration`.
- Never return or log Host Tokens or stored takeover-token hashes.
- Keep takeover dry-run read-only; it must not generate or consume the one-time token.
- Agent takeover restores server-pack, admin-state, then world-snapshot before process start.
- Takeover must not run RCON or safe-sync; it consumes already available artifacts.
- Keep the local two-host demo fake-server based. Do not add a Minecraft installer.
- Do not describe takeover as hot migration or live session transfer. Players reconnect.
- Daemon auto-takeover is opt-in via --auto-takeover flag and reuses the existing takeover.Run flow.
- Artifact GC (retention) is manual API/CLI only; never schedule automatic GC.
- Coordinator local state persistence to JSON is implemented. PostgreSQL is not.

## Useful issues

- #1 V1 MVP flow and non-goals
- #2 Snapshot manifest and server pack format
- #3 Coordinator backend
- #5 Agent CLI
- #7 Safe snapshot sync
- #9 Host election
- #11 End-to-end takeover demo
- #26 Network / Relay architecture groundwork

## PR35: V1 release checklist and release notes

V1 release preparation documentation has been added:

- `docs/v1-release-checklist.md` — 10-section smoke checklist covering Coordinator, Agent, relay E2E, demo, packaging, CI workflow, git hygiene, secrets, user flow, and known limitations
- `docs/v1-release-notes.md` — comprehensive release notes: capabilities (Coordinator, Agent, relay/proxy), local demo, release packaging, security, limitations, next milestones

## PR34: Release artifact workflow

GitHub Actions workflow for agent release artifacts has been added:

- `.github/workflows/agent-release-artifacts.yml` — runs `build-agent-release.sh` and uploads `dist/` as a workflow artifact
- Triggered manually (`workflow_dispatch`) or on `v*` tag push
- Artifact name: `acbh-agent-release-artifacts`
- Does not publish a GitHub Release; artifacts are temporary CI artifacts
- `docs/release-packaging.md` updated with CI workflow instructions

## PR33: Agent release packaging

Agent release packaging scripts and docs have been added:

- `scripts/build-agent-release.sh` — builds `acbh-agent` and `relay-demo` binaries for linux/amd64, linux/arm64, windows/amd64, darwin/amd64, darwin/arm64
- `docs/release-packaging.md` — how to build, run, and verify artifacts
- Binaries are written to `dist/` (gitignored) with SHA256 checksums
- Uses `CGO_ENABLED=0` for static binaries, `-trimpath -ldflags="-s -w"` for small output

## PR32: Relay-only local demo runner

A self-contained local demo runner has been added:

- `agent/cmd/relay-demo/main.go` — in-process demo: relay pair server, TCP echo, Host relay client, Player proxy, test client
- `examples/relay-only-demo/run.sh` — convenience script
- `examples/relay-only-demo/README.md` — documentation with quick start and options
- Tests: single write echo, multi-frame ordering, clean shutdown on Ctrl+C
- Uses random ports by default; host target and player listen addresses are independently configurable
- No real Minecraft server or Coordinator required

## PR31: Relay E2E smoke test

Relay-only end-to-end smoke test has been added:

- `agent/internal/relay/relay_e2e_test.go` — `TestRelayE2ESmoke`, `TestRelayE2ENoSecretsInErrors`, `TestRelayE2ECancellationNoHang`
- Verifies full relay chain: TCP client -> Player local proxy -> relay pair server -> Host relay client -> TCP echo server
- Tests: single write echo, multiple frame ordering, no secrets in errors, cancellation does not hang

## PR29: Player local TCP proxy

Player local TCP proxy has been implemented:

- `acbh-agent relay player` command listens locally and forwards to Coordinator player relay WebSocket
- `agent/internal/playerproxy/proxy.go` -- PlayerProxy with Run, continuous listen, forwarding, cancellation-safe cleanup
- `agent/internal/playerproxy/proxy_test.go` -- 14 tests: auth headers, forwarding, order preservation, close propagation, context cancellation, invalid addresses, secrets exclusion
- CLI tests for required flags, configurable listen address, invalid listen address, secrets exclusion, command registration
- Default listen address is `127.0.0.1:25565`; configurable via `--listen-address` (e.g. `127.0.0.1:25577`)
- Host target address and Player listen address are independent

Next PR should add E2E relay demo and smoke test.

## PR28: Host Agent relay tunnel client

Host Agent relay tunnel client has been implemented:

- `acbh-agent relay host` command connects to Coordinator relay WebSocket and forwards to local TCP target
- `agent/internal/relay/client.go` -- HostRelayClient with WebSocket dial, TCP forward, context cancellation
- `agent/internal/relay/client_test.go` -- 16 tests with in-process WebSocket server and TCP echo
- CLI tests for required flags, invalid addresses, and no secrets in errors
- Flags default from Agent config; target-address is local-only (never exposed)

Next PR should implement the Player local proxy.

## PR27: Public Relay MVP

Public relay MVP byte forwarding has been implemented:

- WebSocket relay endpoints under `/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/{host,player}`
- `RelayManager` in `apps/coordinator/src/relay.ts` tracks active relay pairs and forwards binary frames
- Player session token auth (one-time token, SHA256 hashed, never exposed in tunnel responses)
- Host-side auth with host ID/token/generation for relay connections
- Relay is ephemeral runtime state; not persisted across restarts
- Next PR should connect the Host Agent to the relay WebSocket endpoint

## PR26: Network / Relay architecture

Network/relay architecture groundwork has been added:

- V1 architecture docs: `docs/v1-architecture.md`, `docs/network-design.md`, `docs/tunnel-protocol.md`
- Tunnel/session types in `apps/coordinator/src/network.ts`
- Tunnel store skeleton in `apps/coordinator/src/store.ts` (player sessions, tunnel sessions, expiration)
- Control-plane routes for player session creation and tunnel session planning
- Tunnel sessions bind to `currentHostId` and `currentHostGeneration`
- Tunnel sessions are ephemeral runtime state; NOT persisted across restarts

Next PR should implement relay MVP byte forwarding.
