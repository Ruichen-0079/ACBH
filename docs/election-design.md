# Election Design

V1 election selects a fresh, synchronized candidate and grants it a short-lived takeover assignment. Election does not start Minecraft and does not immediately change the current host.

## Heartbeat data

Hosts may report:

- status and last heartbeat time;
- latest local `world-snapshot`, `server-pack`, and `admin-state`;
- CPU cores, total memory, free disk, and Java availability;
- player connection host, port, and network;
- the legacy `latestLocalSnapshotId` field.

`ACBH_HOST_HEARTBEAT_TIMEOUT_MS` controls freshness and defaults to `30000`.

## Eligibility

A host is eligible when:

- it exists in the group;
- its status is `online` or `standby`;
- its heartbeat is fresh;
- it is not `hosting`, `unhealthy`, or `offline`;
- it reports the latest available world snapshot locally, or no world snapshot exists;
- Java availability is not explicitly false.

Server-pack and admin-state locality increase score but do not hard-block election yet.

## Deterministic Host Score

```text
score = 0
+ 30 if heartbeat is fresh
+ 25 if the latest available world snapshot is local
+ 10 if the latest server pack is local
+  5 if the latest admin state is local
+ 10 if Java is available
+ 10 if total memory is at least 4 GiB
+  5 if free disk is at least 10 GiB
+ min(cpuCores, 8)
+ manualPriority
- recentFailureCount * 10
```

Candidates sort by score descending, heartbeat freshness descending, then host ID ascending using a locale-independent string comparison.

## Election APIs

Authenticated hosts may call:

- `POST /v1/groups/:groupId/election/run`
- `POST /v1/groups/:groupId/election/check-timeout`
- `GET /v1/groups/:groupId/election/status`

Allowed reasons are `manual`, `heartbeat-timeout`, and `no-current-host`.

`check-timeout` returns without election while the current host heartbeat is fresh. A stale current host is marked unhealthy before candidates are evaluated. If there is no current host, the Coordinator runs a `no-current-host` election.

## Takeover assignment

An election winner receives one assignment with status:

```text
offered -> accepted -> completed
                   \-> failed
offered/accepted -> expired or cancelled
```

`ACBH_TAKEOVER_ASSIGNMENT_TTL_MS` defaults to `60000`. A new election cancels the previous active assignment. Only one active assignment exists per group.

The assigned host polls:

- `POST /v1/hosts/takeover/poll`
- `POST /v1/hosts/takeover/accept`
- `POST /v1/hosts/takeover/complete`
- `POST /v1/hosts/takeover/fail`

The takeover token is generated on the first assigned-host poll, returned once, and stored by the Coordinator only as a SHA256 hash. Other hosts receive no assignment or token. Accept, complete, and fail require both host authentication and the takeover token.

## Current-host generation

Each group starts with `currentHostGeneration = 0`. An assignment captures the current generation. Only successful completion:

1. sets `currentHostId` to the assigned host;
2. increments `currentHostGeneration`;
3. marks the assigned host `hosting`.

Election and acceptance do not finalize the host. A stale-generation assignment cannot complete.

## Agent execution

`acbh-agent takeover run` performs:

1. poll;
2. accept;
3. restore `server-pack`;
4. restore `admin-state`;
5. restore `world-snapshot`;
6. start the configured local server process;
7. send a `hosting` heartbeat;
8. complete the assignment.

Missing artifact kinds are skipped. Deletes are applied by default so the restored directory matches assigned manifests. Any failure after acceptance reports a concise failure reason and exits non-zero.

This is restore plus process start, not hot migration. No live player session is transferred, players must reconnect, and the Coordinator never runs Minecraft.

## Daemon auto-takeover (opt-in)

`acbh-agent daemon --auto-takeover=true` extends the heartbeat loop to automatically poll and execute takeover assignments without human intervention.

- The daemon starts by checking `GET /v1/groups/:groupId/election/status`. If this host is already the current host, it sends `hosting` heartbeats and skips all takeover polling.
- On each heartbeat cycle (or per `--takeover-interval`), the daemon polls for a takeover assignment. When one is found, it runs the full `takeover.Run` flow (poll → accept → restore artifacts → start server → heartbeat `hosting` → complete).
- After a successful takeover, the daemon transitions to `hosting` heartbeats and permanently stops polling.
- `takeover.Run` remains the sole owner of poll/accept/fail/complete. The daemon never calls PollTakeover directly — it delegates to `takeover.Run`.
- Failures after accept are reported via `FailTakeover` by `takeover.Run`. Failures before accept (poll error, no assignment, accept rejected) do not generate a fail report.
- A concurrency guard (`atomic.Bool`) ensures only one takeover runs at a time. If a takeover is already in progress, the daemon skips the current poll window.
- `--auto-takeover` requires `--server-dir` and `--command`. Without `--auto-takeover`, daemon behavior is unchanged from prior releases.



## Split-brain boundary

The assignment token and generation form the current V1 lease boundary. They reduce accidental stale completion, but the Coordinator remains in-memory and there is no distributed consensus or network fencing in this milestone.

## Stale-host artifact protection

Only the current host may record new artifact metadata (publish manifests) and potentially advance the latest pointer. The `recordArtifactFromHost` method enforces:

- When `currentHostId` is null (initial group state), any authenticated host may publish without a generation header.
- When `currentHostId` is non-null, the `X-ACBH-Host-Generation` header is required. Missing or malformed headers return 400.
- When the header is present and matches, the publish proceeds normally.
- When the host does not match `currentHostId`, rejecting with 403.
- When the generation does not match `currentHostGeneration`, rejecting with 409.
- On any rejection (403/400/409), the latest pointer is unchanged.

Raw SHA256 object blob uploads are NOT restricted, allowing standby hosts to pre-warm the Coordinator's object storage before an election promotes them.

## Artifact GC protection

Artifact garbage collection (`POST /v1/groups/:groupId/artifacts/gc`) respects all takeover assignment protections:

- Artifacts referenced in `latestArtifactsAtAssignment` of an active (offered/accepted) takeover assignment are **never deleted**, even if they fall outside retention windows.
- This ensures a takeover in progress always has its assigned artifacts available, regardless of GC timing.
