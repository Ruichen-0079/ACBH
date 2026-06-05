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

## Documentation

- `docs/architecture.md`
- `docs/dependency-plan.md`
- `docs/mvp-scope.md`
- `docs/sync-design.md`
- `docs/election-design.md`
- `docs/security.md`
- `docs/codex-guide.md`
