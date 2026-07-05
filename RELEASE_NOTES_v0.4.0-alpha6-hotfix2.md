# ACBH v0.4.0-alpha6-hotfix2

Hotfix2 for the v0.4.0-alpha6 Desktop + Coordinator release.

## Highlights

- **Coordinator bundle packaging**: `acbh-coordinator-linux-amd64-bundle.tar.gz` now ships with `VERSION`, `SHA256SUMS`, `acbh-vps-lib.sh`, `acbh-vps-rollback.sh`, and LF-normalized shell scripts for reliable VPS install/upgrade.
- **Protocol v2 preserved**: Coordinator `/health` and `/v1/capabilities` remain on protocol version `2` with the full alpha6 capability set (`capabilities_v1`, `desktop_protocol_v2`, `world_backup_resume`, `public_relay_v1`, `bootstrap_packages_v1`, and related lease/backup/invite features).
- **Desktop lease fixes**: Initialization and long-running backup operations keep the current host lease renewed; lease failures surface clearer error codes.
- **Snapshot UI**: Backup tab lists remote snapshots, supports manual selection, and routes restore/download/sync through the selected snapshot or the `latest` shortcut.
- **Restore validation**: Download-to-new-directory requires a non-empty restore target before starting the operation.
- **Backup profile hydration**: Summary refresh repopulates profile id/name, preset, backup roots, and exclude patterns from the active server profile.
- **Stale server lock banner**: Server tab shows stale lock diagnostics from `/api/server/status` with a safe `repair-state` action.
- **Operations queue detail**: Operation list and result panels show `operationId`, `errorCode`, `message`, `traceId`, and upload/restore progress.

## Compatibility

- Coordinator version string: `0.4.0-alpha6-hotfix2` (read from `package.json`).
- Agent/Desktop version string: `v0.4.0-alpha6-hotfix2`.
- No VPS storage wipe or incompatible storage migration is required.
- Build artifacts are emitted under `dist/v0.4.0-alpha6-hotfix2/` when `VERSION` is set.

## Verification

- `go test ./...`
- `pnpm --filter @acbh/coordinator test`
- `VERSION=v0.4.0-alpha6-hotfix2 bash scripts/build-agent-release.sh`