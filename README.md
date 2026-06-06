# ACBH — Anyone Can Be Host

ACBH is a distributed Minecraft host handoff platform.

It allows a Minecraft server to be restarted and taken over by another trusted player device when the current host goes offline. V1 is not seamless hot migration. It is file-level snapshot synchronization plus fast host takeover.

## V1 promise

```text
Host A goes offline.
Coordinator elects Host B.
Host B restores the latest verified snapshot.
Host B starts the Minecraft server.
Players reconnect.
```

Target V1 recovery time: **10–30 seconds**.

## Non-goals

- No live JVM memory migration.
- No tick/entity/redstone/mod runtime state migration.
- No transparent player session transfer.
- No Minecraft mod/plugin dependency.
- No proxy layer in the first MVP.

## Repository layout

```text
ACBH/
├── apps/
│   └── coordinator/      # TypeScript Coordinator service
├── agent/                # Go cross-platform Agent CLI
├── docs/                 # Architecture and protocol documents
├── examples/             # Local demo and deployment examples
└── .github/workflows/    # CI
```

## Main components

### Coordinator

A public lightweight service responsible for groups, members, hosts, heartbeats, snapshots, storage metadata, and host election. It does not run Minecraft.

### Agent

A client-side daemon/CLI installed on candidate host devices. It downloads server packs, starts Minecraft, performs safe sync, uploads snapshots, reports health, and executes takeover.

### Storage

A content-addressed file store for server packs, snapshot manifests, and file blobs. V1 starts with local filesystem storage; S3-compatible storage can be added later.

## First local targets

```bash
pnpm install
pnpm dev:coordinator

cd agent
go run . --help
go run . doctor
```

## In-memory Coordinator API example

The first Coordinator API keeps all state in memory. Restarting the Coordinator clears groups, members, hosts, access-key hashes, and host-token hashes.

Start the Coordinator:

```bash
pnpm dev:coordinator
```

Create a group. Save the returned `groupId`, `ownerMemberId`, and one-time `accessKey`:

```bash
curl -s http://localhost:6121/v1/groups \
  -H "content-type: application/json" \
  -d '{"name":"Survival Server","ownerName":"Owner"}'
```

Join the group with the one-time access key value:

```bash
curl -s http://localhost:6121/v1/groups/<groupId>/join \
  -H "content-type: application/json" \
  -d '{"accessKey":"<accessKey>","displayName":"PlayerA"}'
```

Register a host candidate device. Save the returned one-time `hostToken`:

```bash
curl -s http://localhost:6121/v1/hosts/register \
  -H "content-type: application/json" \
  -d '{"groupId":"<groupId>","memberId":"<memberId>","deviceName":"PlayerA-PC","platform":"windows","agentVersion":"0.1.0"}'
```

Send a heartbeat:

```bash
curl -s http://localhost:6121/v1/hosts/heartbeat \
  -H "content-type: application/json" \
  -d '{"groupId":"<groupId>","hostId":"<hostId>","hostToken":"<hostToken>","status":"standby","latestLocalSnapshotId":null}'
```

Inspect debug state. This does not return access keys or host tokens:

```bash
curl -s http://localhost:6121/v1/groups/<groupId>/state
```

## Agent CLI example

The Agent can join the in-memory Coordinator, register the local device, store its local config, and send heartbeats.

Check local diagnostics:

```bash
cd agent
go run . doctor
```

After creating a group with the Coordinator API, log in with the returned one-time access key:

```bash
go run . login \
  --coordinator http://localhost:6121 \
  --group-id <groupId> \
  --access-key <accessKey> \
  --name PlayerA \
  --device-name PlayerA-PC \
  --platform windows
```

The Agent stores config at `<user config dir>/acbh/config.yaml`. It does not print the host token after storing it.

Send one heartbeat:

```bash
go run . heartbeat --status standby
```

Run the heartbeat loop:

```bash
go run . daemon --interval 10s --status standby
```

Then inspect Coordinator state:

```bash
curl -s http://localhost:6121/v1/groups/<groupId>/state
```

## Agent local manifest examples

The Agent can scan a local Minecraft server directory and generate manifests for one artifact kind at a time. This is local manifest generation only; it does not upload files and it is not RCON safe sync.

Generate a world snapshot manifest. World snapshots include `world-runtime` and `plugin-runtime-data` files. Deleted entries from the previous manifest are written with `deleted: true`, `size: 0`, and an empty `sha256`.

```bash
cd agent
go run . scan \
  --server-dir C:/minecraft/server \
  --artifact-kind world-snapshot \
  --artifact-id snap_000001 \
  --server-pack-version pack_000001 \
  --group-id <groupId> \
  --creator-host-id <hostId> \
  --output ./snap_000001.manifest.json
```

Generate a server pack manifest:

```bash
go run . scan \
  --server-dir C:/minecraft/server \
  --artifact-kind server-pack \
  --artifact-id pack_000001 \
  --group-id <groupId> \
  --creator-host-id <hostId> \
  --output ./pack_000001.manifest.json
```

Validate, inspect, and diff manifests:

```bash
go run . manifest validate --file ./snap_000001.manifest.json
go run . manifest inspect --file ./snap_000001.manifest.json
go run . manifest diff --old ./snap_000001.manifest.json --new ./snap_000002.manifest.json
```

If the Agent config already exists, `scan` can read the group ID and creator host ID from local config. Explicit flags override config values. Unknown and ignored files are counted in the scan report but are never included in manifests.

### RCON safe sync

Plain `scan` reads local files without coordinating with a running Minecraft server. Before generating a `world-snapshot` from a live server, enable RCON in `server.properties`:

```properties
enable-rcon=true
rcon.port=25575
rcon.password=change-me
```

Then run `safe-sync`. It authenticates to RCON, sends `save-all flush`, waits for a successful response, and only then scans the server directory:

```bash
go run . safe-sync \
  --server-dir C:/minecraft/server \
  --artifact-id snap_000001 \
  --server-pack-version pack_000001 \
  --output ./snap_000001.manifest.json \
  --rcon-host 127.0.0.1 \
  --rcon-port 25575 \
  --rcon-password change-me
```

Instead of placing the password in command history, set `ACBH_RCON_PASSWORD` and omit `--rcon-password`. The flag takes precedence when both are present. The password is not saved in Agent config or printed.

`safe-sync` only generates a `world-snapshot` manifest. Upload remains a separate step:

```bash
go run . push \
  --manifest ./snap_000001.manifest.json \
  --server-dir C:/minecraft/server
```

Push a scanned manifest and its file objects to the Coordinator local storage backend:

```bash
go run . push \
  --manifest ./snap_000001.manifest.json \
  --server-dir C:/minecraft/server
```

Push streams file objects as `application/octet-stream` by default, so files are not base64-encoded or held entirely in Agent memory. The compatibility JSON/base64 upload can be selected for small test artifacts only:

```bash
go run . push \
  --manifest ./snap_000001.manifest.json \
  --server-dir C:/minecraft/server \
  --legacy-json-upload
```

Pull the latest world snapshot and restore files into a separate directory:

```bash
go run . pull \
  --artifact-kind world-snapshot \
  --artifact-id latest \
  --output-dir ./restore
```

Deleted manifest entries are reported but not applied by default. To remove files listed as deleted entries:

```bash
go run . pull \
  --artifact-kind world-snapshot \
  --artifact-id latest \
  --output-dir ./restore \
  --apply-deletes
```

Object uploads and downloads use binary streaming by default. `ACBH_MAX_OBJECT_BYTES` sets the Coordinator upload limit in bytes and defaults to `268435456` (256 MiB). `POST /v1/artifacts/objects` remains a 16 MiB JSON/base64 compatibility endpoint for testing only. Manifest upload has a 1 MiB request body limit.

Current transfers are not resumable. Interrupted objects must be transferred again; resumable chunks and remote object storage remain future work.

## Documentation

- `docs/architecture.md`
- `docs/dependency-plan.md`
- `docs/mvp-scope.md`
- `docs/sync-design.md`
- `docs/election-design.md`
- `docs/security.md`
- `docs/codex-guide.md`
