# ACBH v0.4 Relay-First smoke results

Validation date: 2026-07-16 (Asia/Shanghai)

## Revision and topology

- Branch: `rewrite/v0.4-relay-first-clean`
- Base: `81c5e886f11fb973cb0f22a0b7fc7ed754529cfa`
- Test Minecraft: `127.0.0.1:25575`
- Public player endpoint: `121.40.101.224:25575`
- Test Coordinator: `121.40.101.224:6122`
- Test frps control port: `121.40.101.224:7001`
- Protected existing Minecraft: `127.0.0.1:25565`

The supplied isolated server already listened on 25575, so the test used the
allowed same-port mapping. ACBH did not read or modify `server.properties`.

## Automated validation

- `go test ./...`: pass
- `go vet ./...`: pass
- Coordinator build: pass
- Coordinator full tests after PR integration: 155/155 pass
- Embedded single-page UI: inspected in a real local browser
- Legacy source audit: no WebSocket player relay, Desktop runtime state writer,
  or `remote-public` state machine in the v0.4 Agent path

## Failure and recovery smoke

| Scenario | Result |
|---|---|
| Initial isolated start | ONLINE in about 12.30 seconds |
| Close GUI | Agent, Minecraft, and frpc continued |
| Restart test Coordinator | Data plane stayed up; recovered in about 5.02 seconds |
| Kill managed frpc | One frpc recovered in about 6.84 seconds |
| Isolated frps outage | Relay reported RECONNECTING and recovered |
| Wrong token | Terminal AUTH_FAILED; no retry storm or disclosure |
| Public port conflict | Terminal PUBLIC_PORT_IN_USE in about 0.12 seconds |
| Restart Agent | Desired state reconciled without duplicate processes |
| Minecraft crash attempts 1-3 | Recovered in about 12.58, 14.17, and 13.15 seconds |
| Minecraft crash attempt 4 | API-visible restart_limit_reached in about 1.35 seconds |
| Duplicate Start and Stop | Same operation; no duplicate Java or frpc |

## Minecraft client validation

Client validation through the public endpoint passed. Privacy-minimized server
log evidence records:

- player: `Ruichen_0079`;
- first completed login/world join: `2026-07-16 22:07:59 +08:00`;
- first logout: `2026-07-16 22:08:25 +08:00`;
- completed reconnect/world join: `2026-07-16 22:08:42 +08:00`;
- final observed logout: `2026-07-16 22:10:23 +08:00`.

The user confirmed world entry, movement, block interaction, exit, and
reconnect. Relay stayed ONLINE. No player IP is recorded.

## Protected local 25565

The existing server retained PID `29556`, process start time
`2026-07-16T10:11:47.2081788Z`, listener connectivity, and unchanged server
directory timestamp throughout the smoke and client tests. Its world and log
timestamps advanced normally while it remained online. No test action targeted
that PID or directory.

## Credential handling

- The configured credential was absent from local logs, diagnostics, logs API,
  and config API response.
- The deliberately wrong credential was absent from logs.
- No private-key content or path and no player IP is present in monitor output
  or this report.

## Completed 16-hour durability threshold

The unattended run was intentionally ended and accepted as a 16-hour
durability threshold:

- Windows: 16.52 continuous hours, 980 samples;
- VPS: 16.56 continuous hours, 992 samples;
- sampling gaps longer than 90 seconds: 0 on both monitors;
- Agent, Minecraft, Relay, and Coordinator non-healthy samples: 0;
- frps non-active samples: 0;
- public 25575 probe failures: 0;
- protected local 25565 listener failures or PID changes: 0;
- maximum isolated test Java count: 1;
- maximum managed frpc count: 1.

Windows Agent resource peaks were about 59.4 MiB working set, 20 threads, and
259 handles. VPS peaks were about 73.4 MiB for Coordinator and 31.2 MiB for
frps. The Windows process and VPS systemd monitor were stopped after final
summary collection. Minecraft, managed frpc, Coordinator, frps, and the
authorized UFW rules remain running.

This result passes the agreed **16-hour durability threshold**. It does not
claim that a 24-hour run was completed. The recommended sequence is to merge
and release `v0.4.0-rc1` after PR review, with any later final-release endurance
requirement recorded separately.

## PR #68 integration candidate smoke

The final runtime candidate `9669774642f244afe98f53a674cb54c2bcea37b2`
merged `origin/main` into the relay-first branch without selecting whole-file
`ours` or `theirs` resolutions. The candidate retained FRP as the Hobby player
data plane, kept Coordinator HTTP as the control plane, and did not connect the
Hobby entry path to the legacy WebSocket/playerproxy, Desktop runtime, or
`remote-public` state machines.

The candidate was deployed only to the isolated test Agent and Coordinator.
The existing `frpc`/`frps` version, ports, token, VPS services, UFW rules, and
protected local 25565 server were unchanged. Results were:

- Coordinator restart: the Relay remained ONLINE and every observed public
  25575 availability probe succeeded; Coordinator recovered without restarting
  Minecraft or `frpc`.
- Exact managed-frpc termination: PID 57200 was replaced by PID 40876 in about
  six seconds; the observed managed-frpc count never exceeded one.
- Duplicate Stop and Start calls returned the same operation IDs, completed
  successfully, and returned to one test Java and one managed `frpc`.
- Minecraft client validation after candidate deployment: player
  `Ruichen_0079` completed a world join at `2026-07-17 15:52:10 +08:00`, left,
  completed a reconnect/world join at `15:52:20`, and left normally. The user
  confirmed successful entry and reconnect. No player IP is retained.
- Thirty-minute post-fault window: 31 samples over 30.45 minutes, zero
  non-healthy Agent/Minecraft/Relay/Coordinator samples, zero public 25575
  probe failures, zero protected-25565 failures or PID changes, maximum one
  test Java, and maximum one managed `frpc`.
- Candidate Agent resource peaks in that window: about 56.3 MiB working set,
  18 threads, and 248 handles.
- The protected local 25565 listener remained PID 29556 throughout.

GitHub Actions run
[`29563142494`](https://github.com/Ruichen-0079/ACBH/actions/runs/29563142494)
passed the Linux and Windows Agent/Coordinator matrix. The repository Agent,
Coordinator, and VPS-script checks on PR #68 also passed. The isolated test
environment remains running; this short smoke does not replace or extend the
accepted 16-hour durability result and does not claim a 24-hour run.
