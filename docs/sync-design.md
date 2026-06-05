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

## Future file classes

Future scanners should classify files before deciding which artifact namespace they belong to:

- `world-runtime`
- `server-pack`
- `admin-state`
- `plugin-runtime-data`
- `ignored`
- `unknown`

The classifier is not implemented yet. Future work must avoid mixing server-pack changes with world snapshots, and unknown files should not be silently promoted into an approved artifact.
