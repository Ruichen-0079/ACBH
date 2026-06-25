# Desktop alpha3 refactor

Target version: v0.4.0-alpha3.

## UI technology

The production Windows desktop entry is now the Go `acbh-desktop-windows-amd64.exe`
runtime. It starts a loopback HTTP UI from embedded Go assets and opens it with
the OS URL handler. The UI does not require PowerShell, `pwsh`, ThreadJob, Node,
or a developer toolchain to render. The local Coordinator can still use the
bundled Node runtime when the user chooses local private mode.

The legacy PowerShell WinForms GUI remains in the repository for development
fallback only and is launched explicitly with `--legacy-powershell-gui`. It is
not copied into the production Windows bundle.

## Operation manager

All UI actions enter a Go `OperationManager`. Each operation has:

- `operationId`, `name`, `mutexClass`, `state`, `startedAt`, `completedAt`
- `currentStage`, `progress`, `cancellable`, `timeout`, `traceId`
- exactly one terminal envelope

Busy state is derived from queued/running operations. Mutually exclusive classes
cover server start/stop, backup/restore, and invite mutation. Read-only refreshes
use coalescing classes so repeated clicks share the active operation.

Each operation owns a context with a deadline. Cancellation, timeout, and server
shutdown release the context and mutex in the same completion path. Success is
published only after the operation function returns and the terminal envelope is
created.

## Agent communication

The Go desktop runtime calls `internal/desktop` services directly. UI responses
use the protocol v2 envelope:

```json
{
  "schemaVersion": 2,
  "ok": true,
  "outcome": "success",
  "errorCode": "",
  "message": "",
  "warnings": [],
  "data": {},
  "traceId": "",
  "startedAt": "",
  "completedAt": ""
}
```

Warnings map to `success_with_warnings` with `ok=true`. Business failures keep
`ok=false` and an explicit `errorCode`; Cobra usage is not part of GUI logs.

Progress events are stored on the operation:

```json
{
  "type": "progress",
  "operationId": "op_...",
  "stage": "uploading",
  "message": "正在上传世界对象",
  "current": 12,
  "total": 90
}
```

## Bootstrap state machine

Startup runs a single ordered state machine:

1. RuntimeCheck
2. LoadLocalConfig
3. EnvironmentCheck
4. CoordinatorHandshake
5. ResolveIdentity
6. RefreshServerStatus
7. RefreshLeaseStatus
8. RefreshWorldBackupStatus
9. RefreshInviteCapability
10. Ready

Remote non-critical failures produce a degraded but operable desktop state.

## Coordinator capability handshake

`GET /v1/capabilities` returns the Coordinator version, protocol version,
minimum client protocol, capability names, server time, and authentication mode.
`/health` also includes protocol metadata.

Alpha3 features are gated by capability names such as `world_backup_v1`,
`world_backup_resume`, `invite_management_v1`, `public_relay_v1`,
`lease_renew_v1`, and `bootstrap_packages_v1`.

## Identity and invite model

The Coordinator is the authority for group role. The desktop calls `whoami` with
host credentials and compares the authenticated server role to any local cached
role. Invite create/list/revoke use owner/admin host authentication and no longer
depend on the current Minecraft host lease.

If local role and server role disagree, the desktop returns `identity_mismatch`
so the user can re-authenticate instead of seeing a misleading permission error.

## Host lease model

Lease status distinguishes `currentHostIdMatches`, `leaseValid`,
`leaseExpiresAt`, `leaseRemaining`, `generation`, `serverTime`, and
`heartbeatActive`. `isCurrentHost` is true only when the host ID matches and the
lease is still valid.

Before destructive world backup operations, the desktop calls
`EnsureActiveLease`. The Coordinator renews or reacquires when no fresh host is
holding the lease, and returns structured errors such as
`lease_held_by_other_host` or `stale_host_generation` otherwise.

## Logging

The UI shows operation summaries. Full envelopes are written to
`%APPDATA%\ACBH\logs\desktop-debug.log` with size-based rotation. Secret-looking
fields are redacted, including host/member tokens, invite codes, RCON passwords,
access keys, and takeover tokens.

## Migration and rollback

Existing CLI commands and desktop config files remain compatible. Users can run
the old script manually from the repository with `--legacy-powershell-gui` during
development, but alpha3 release bundles use the Go runtime by default. Rollback
to alpha2 is a release-level downgrade; alpha2 tags and releases are not modified
by this refactor.
