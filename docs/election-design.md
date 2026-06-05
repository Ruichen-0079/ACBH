# Election Design

V1 election selects an online, healthy, synchronized candidate when the current host is unavailable.

## Host states

- `offline`
- `online`
- `standby`
- `hosting`
- `unhealthy`

## Eligibility

A host is eligible when:

- its member role is `host_candidate`, `admin`, or `owner`;
- it has recent heartbeat;
- it has the latest snapshot or can pull it;
- it has enough disk space;
- Java is available;
- it is not marked unhealthy due to repeated crashes.

## Basic Host Score

Initial scoring can be simple and deterministic:

```text
score = 0
+ 30 if heartbeat is fresh
+ 25 if latest snapshot is local
+ 15 if memory is sufficient
+ 10 if disk is sufficient
+ 10 if Java is available
+ 10 manual priority placeholder
```

V1 should prefer correctness over clever scoring.

## Takeover flow

1. Heartbeat monitor detects timeout.
2. Current host is marked `unhealthy` or `offline`.
3. Coordinator loads eligible candidates.
4. Coordinator sorts candidates by Host Score.
5. Coordinator grants takeover to one candidate.
6. Selected Agent pulls latest verified snapshot.
7. Selected Agent starts Minecraft.
8. Selected Agent reports `hosting`.
9. Coordinator sets `current_host_id` to the new host.

## Split-brain avoidance

Only one host should be `hosting` for a group.

Rules:

- Coordinator should issue a takeover token for a specific host.
- Agent should not start takeover without Coordinator assignment.
- Old host rejoining after timeout must not overwrite the current host.
- Snapshot uploads from stale hosts should be rejected or quarantined until manually reviewed.
