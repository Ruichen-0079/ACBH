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

### Local process manager

The Agent process manager starts and stops a user-provided command in a configured local server directory. A small detached local supervisor keeps the child stdin available, appends stdout and stderr to local log files, and owns the runtime state used by `server status` and `server stop`.

Process control remains entirely on the Agent device. The Coordinator does not launch, stop, or supervise Minecraft. The process manager does not select hosts or perform automatic takeover.

### Takeover executor

The Agent polls a Coordinator assignment, stores its one-time token in local runtime state, restores assigned artifacts, starts the local server process, reports `hosting`, and completes the assignment. Artifact restore order is server pack, admin state, then world snapshot.

The Coordinator only manages control-plane records. It never runs Minecraft. Takeover transfers files and starts a new process; it does not transfer JVM memory or live player sessions, so players reconnect.

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
4. Coordinator offers a generation-scoped takeover assignment.
5. Candidate accepts and pulls assigned artifacts.
6. Candidate starts Minecraft.
7. Candidate reports `hosting` and completes the assignment.
8. Coordinator updates `currentHostId` and increments `currentHostGeneration`.

Election does not change the current host. Only successful assignment completion does.

## V1 network model

V1 assumes hosts may be behind NAT. ACBH should document and support external Tailscale/Headscale use instead of building custom NAT traversal.

The Coordinator stores current host connection metadata. A proxy or relay can be added after the core handoff loop works.
