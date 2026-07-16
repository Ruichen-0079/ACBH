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
- Coordinator full tests: 51/51 pass
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

## Unattended durability run

The smoke prerequisites for starting the 24-hour run are met. Windows and VPS
monitors now record JSONL every 60 seconds, rotate at 20 MiB, and retain five
files. See [v0.4-durability-monitoring.md](v0.4-durability-monitoring.md) for
start, status, stop, and collection commands.

The durability run has started but has **not completed**. This report does not
claim that the 24-hour test passed. The recommended sequence is to merge and
release `v0.4.0-rc1` after PR review, then require a successful 24-hour report
before the final `v0.4.0` release.
