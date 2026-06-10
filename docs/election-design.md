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

## Split-brain boundary

The assignment token and generation form the current V1 lease boundary. They reduce accidental stale completion, but the Coordinator remains in-memory and there is no distributed consensus or network fencing in this milestone.
