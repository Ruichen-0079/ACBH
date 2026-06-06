# Sync Design

ACBH V1 uses file-level synchronization. It does not parse Minecraft chunks, entities, redstone state, or mod internals.

## Safe sync principle

Minecraft world files can be corrupted if copied while the server is writing them. Therefore V1 safe sync must use RCON before scanning files.

Safe sync flow:

1. Connect to local Minecraft RCON.
2. Send `save-all flush`.
3. Wait for command completion or timeout.
4. Scan configured sync paths.
5. Generate SHA256 hashes.
6. Compare with previous manifest.
7. Upload changed files.
8. Upload manifest.
9. Coordinator marks snapshot as available only after verification.

## Required sync paths

- `world/`
- `world_nether/`
- `world_the_end/`
- `server.properties`
- `whitelist.json`
- `ops.json`
- `banned-players.json`
- `banned-ips.json`
- `usercache.json`

The following paths are usually inside world directories but should be included by the scanner:

- `playerdata/`
- `advancements/`
- `stats/`
- `data/`
- `region/`
- `entities/`
- `poi/`

## Manifest draft

```json
{
  "snapshotId": "snap_000001",
  "groupId": "group_abc",
  "serverPackVersion": "pack_000001",
  "parentSnapshotId": null,
  "createdAt": "2026-06-05T00:00:00Z",
  "creatorHostId": "host_abc",
  "files": [
    {
      "path": "world/region/r.0.0.mca",
      "size": 4194304,
      "sha256": "...",
      "modifiedAt": "2026-06-05T00:00:00Z",
      "deleted": false
    }
  ]
}
```

## Dirty-file tracker

Filesystem events are useful for reducing scan cost, but they are not proof that a file is safe to upload.

Rules:

- `fsnotify` events only mark files as dirty.
- Upload must happen after safe sync.
- If dirty tracking is unreliable, fall back to full manifest scan.

## Snapshot validity

A snapshot is valid only when:

- manifest JSON is parseable;
- every non-deleted file has a SHA256 hash;
- uploaded object hash matches manifest hash;
- required metadata is present;
- Coordinator marks the snapshot `available`.

Rejected or partial snapshots must never become the latest snapshot.

## Local manifest scanner and safe sync

`acbh-agent scan` is a local file scan only. It classifies files, hashes included files, validates manifest shape, compares manifests, and records deleted files when a previous manifest is provided. It does not make a running Minecraft server safe to copy.

`acbh-agent safe-sync` is restricted to `world-snapshot`. It authenticates to the configured Minecraft RCON endpoint, sends `save-all flush`, waits for a successful response, and then invokes the same scanner. If connection, authentication, command execution, or timeout fails, no scan is performed.

Deleted manifest entries use:

- `deleted: true`
- `size: 0`
- `sha256: ""`

The RCON password comes from `--rcon-password` or `ACBH_RCON_PASSWORD`. It is not stored in Agent config or printed.

## Push and pull

The networked artifact sync flow separates scan, safe sync, push, and pull:

1. `acbh-agent scan` generates a local manifest when the caller knows files are already safe to read.
2. `acbh-agent safe-sync` runs RCON `save-all flush`, then generates a world-snapshot manifest.
3. `acbh-agent push` uploads referenced file objects, then uploads the manifest.
4. Coordinator verifies that every non-deleted file object exists before marking the artifact `available`.
5. Coordinator updates the latest pointer only for available artifacts.
6. `acbh-agent pull` downloads the manifest and file objects, verifies SHA256, and restores under the requested output directory.

Pull does not apply deleted entries unless `--apply-deletes` is set.

The current live-server flow is `safe-sync -> push`. These are separate commands, and this remains file-level snapshot synchronization rather than hot migration.

## File classes

Scanners classify files before deciding which artifact namespace they belong to:

- `world-runtime`
- `server-pack`
- `admin-state`
- `plugin-runtime-data`
- `ignored`
- `unknown`

Artifact kinds stay separate:

- `world-snapshot` includes `world-runtime` and `plugin-runtime-data`.
- `server-pack` includes `server-pack`.
- `admin-state` includes `admin-state`.

Ignored and unknown files are counted for diagnostics but are not included in manifests. Server-pack, admin-state, and world snapshot changes must not be mixed into one artifact.
