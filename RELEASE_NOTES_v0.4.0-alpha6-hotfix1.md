# ACBH v0.4.0-alpha6-hotfix1

Hotfix release for the v0.4.0-alpha6 Desktop + Coordinator protocol.

## Highlights

- Desktop now renews the current host lease during initialization and long-running operations.
- World backup upload/restore/download paths keep the lease alive and fail with clearer lease error codes.
- Snapshot UI now lists remote world snapshots and supports manual selection, dry-run, restore, and download-to-new-directory flows.
- Download-to-new-directory restore no longer treats ordinary top-level files such as `banned-ips.json` as symlink escapes.
- Backup profile presets and custom file/directory choices persist across refresh and Desktop restart.
- Launch file, Java path, and working directory persist and are used by StartServer.
- Stale `server.lock` diagnostics now expose repairability, PID, command line, log paths, and log tails.
- Desktop provides a safe repair-state entry that refuses to remove state while the recorded process is alive or unverifiable.

## Compatibility

- Coordinator `/health` and `/v1/capabilities` remain on `v0.4.0-alpha6`.
- Protocol version remains `1`.
- Existing alpha6 capabilities are preserved:
  - `lease_renew_v1`
  - `world_backup_v1`
  - `group_whoami_v1`
  - `invite_management_v1`
- No VPS storage wipe or incompatible storage migration is required.

## Verification

- `go test ./...`
- `tsc -p apps/coordinator/tsconfig.json`
- Coordinator `node --test --import tsx ...`
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\acbh-desktop-gui.ps1 -SelfTest`

