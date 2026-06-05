# ACBH V1 MVP Scope

## Definition

ACBH V1 is a file-level Minecraft host handoff system.

It does not attempt true hot migration. It restores a Minecraft server from the latest verified snapshot and starts it on a newly elected host.

## End-to-end flow

1. Owner creates a group.
2. Owner uploads a server pack and initial snapshot.
3. Candidate hosts install the Agent and join the group.
4. Current Host starts the real Minecraft server.
5. Current Host sends heartbeat to Coordinator.
6. Current Host periodically performs safe sync.
7. Coordinator stores latest verified snapshot metadata.
8. Standby hosts pull missing files.
9. Current Host exits, crashes, or times out.
10. Coordinator elects a synchronized healthy candidate.
11. New Host restores latest verified snapshot.
12. New Host starts Minecraft and reports online.
13. Players reconnect.

## V1 goals

- Group creation and join flow.
- Host registration and heartbeat.
- Server pack metadata.
- Safe snapshot sync using RCON `save-all flush`.
- SHA256-based manifest verification.
- Basic host scoring and election.
- Local filesystem storage backend.
- Host A to Host B takeover demo within 30 seconds.

## V1 non-goals

- No live JVM memory migration.
- No tick state migration.
- No entity runtime state migration.
- No redstone runtime state migration.
- No mod object state migration.
- No transparent player session transfer.
- No custom proxy or relay layer.
- No GUI launcher.
- No chunk-level semantic diff.

## Success criterion

```text
Host A shuts down.
Host B starts the same Minecraft server from the latest verified snapshot within 30 seconds.
```
