# Architecture

ACBH consists of three main components: Coordinator, Agent, and Storage.

## Coordinator

The Coordinator is a lightweight public service. It does not run Minecraft.

Responsibilities:

- Group creation and membership state
- Role and token checks
- Host registration
- Host heartbeat
- Host score calculation
- Host election
- Snapshot metadata
- Storage coordination
- Current host connection metadata

## Agent

The Agent runs on trusted player devices.

Responsibilities:

- Authenticate with a Coordinator group
- Download server pack metadata
- Pull latest verified snapshot
- Start and stop the Minecraft server process
- Send host heartbeat
- Run safe snapshot sync
- Upload changed files and manifest
- Execute takeover when selected
- Report local health

## Storage

Storage contains server packs, snapshot manifests, and file blobs.

V1 local layout:

```text
.acbh-storage/
└── groups/
    └── <groupId>/
        ├── packs/
        │   └── <packVersion>/
        ├── snapshots/
        │   └── <snapshotId>/manifest.json
        └── objects/
            └── sha256/<first-two>/<sha256>
```

## Runtime state

The current host is the only node that runs the writable Minecraft server.

Standby hosts may keep synchronized local copies but must not run writable servers for the same group at the same time.

## Failure recovery

1. Coordinator detects missing heartbeat.
2. Coordinator marks the current host unhealthy.
3. Coordinator selects an eligible candidate.
4. Candidate pulls the latest verified snapshot.
5. Candidate starts Minecraft.
6. Candidate reports `hosting`.
7. Coordinator updates `current_host_id`.

## V1 network model

V1 assumes hosts may be behind NAT. ACBH should document and support external Tailscale/Headscale use instead of building custom NAT traversal.

The Coordinator stores current host connection metadata. A proxy or relay can be added after the core handoff loop works.
