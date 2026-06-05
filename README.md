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

## Documentation

- `docs/architecture.md`
- `docs/dependency-plan.md`
- `docs/mvp-scope.md`
- `docs/sync-design.md`
- `docs/election-design.md`
- `docs/security.md`
- `docs/codex-guide.md`
