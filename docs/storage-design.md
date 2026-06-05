# Storage Design

V1 starts with local filesystem storage and later adds S3-compatible storage.

## Interface goals

Storage should support:

- saving file blobs;
- reading file blobs;
- saving manifest JSON;
- reading manifest JSON;
- verifying SHA256;
- separating group data;
- avoiding path traversal.

## Artifact namespaces

Storage separates three versioned artifact kinds:

- `server-pack`: server jar/core metadata, mods, plugin jars, config, defaultconfigs, kubejs, scripts, and launch metadata.
- `world-snapshot`: world runtime data such as `world/`, `world_nether/`, `world_the_end/`, playerdata, advancements, stats, region/entities/poi/data, and plugin runtime data that changes during gameplay.
- `admin-state`: `server.properties`, `whitelist.json`, `ops.json`, ban files, permissions, and explicit admin state.

Do not treat all server files as one snapshot. A world snapshot must stay separate from server-pack and admin-state changes.

## Content-addressed objects

File blobs should be stored by hash where possible:

```text
objects/sha256/<first-two>/<sha256>
```

This avoids duplicate file storage and makes integrity verification explicit.

## Manifest metadata

Coordinator owns artifact validity. Storage only stores bytes.

An artifact becomes usable only after Coordinator marks it `available`.

## V1 local backend

The first storage backend is local filesystem storage used by the Coordinator.

Default root:

```text
.acbh-storage
```

Override root:

```bash
ACBH_STORAGE_ROOT=/path/to/acbh-storage pnpm dev:coordinator
```

Local layout:

```text
.acbh-storage/
└── groups/
    └── <groupId>/
        ├── server-packs/
        │   └── <packVersion>/
        │       └── manifest.json
        ├── world-snapshots/
        │   └── <snapshotId>/
        │       └── manifest.json
        ├── admin-states/
        │   └── <adminStateVersion>/
        │       └── manifest.json
        └── objects/
            └── sha256/
                └── <first-two>/<sha256>
```

Rules:

- Objects are addressed by SHA256.
- The local backend verifies object bytes against the provided SHA256 before and after writing.
- Group IDs, artifact IDs, artifact kinds, manifest paths, and object hashes are validated before path construction.
- Storage paths must stay under the configured storage root.
- Storage must not contain raw access keys, host tokens, RCON passwords, or other secrets.

Manifest fields reserve:

- `artifactKind`
- `artifactId`
- `groupId`
- `createdAt`
- `creatorHostId`
- `parentArtifactId`
- `serverPackVersion`
- `files`

For `world-snapshot`, `serverPackVersion` ties the world data to the server pack that produced it. For `server-pack`, `serverPackVersion` may equal `artifactId`. `admin-state` stays separate from world snapshots.

This storage layer does not yet implement snapshot upload APIs, artifact approval workflow, RCON safe sync, Minecraft runtime control, host election, remote object storage, or garbage collection.
