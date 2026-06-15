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

## Quick start

```bash
pnpm install
pnpm dev:coordinator
```

In another terminal:

```bash
cd agent
go run . --help
go run . doctor
```

## Run tests

```bash
bash scripts/verify-all.sh       # Linux / Fedora / macOS
powershell scripts/verify-all.ps1 # Windows
```

Or individually:

```bash
# Go
cd agent && go vet ./... && go test ./... -count=1

# Coordinator
pnpm build:coordinator && pnpm --filter @acbh/coordinator test
```

## Run demo smoke

```bash
bash scripts/demo-smoke.sh
```

The demo runs build + health + group + host + heartbeat + scan + push + latest + pull
+ restore verify all locally. No real Minecraft or public network needed.

See [docs/demo.md](docs/demo.md) for detailed walkthrough and troubleshooting.

## Run the graphical demo

Start the Coordinator, then open `http://127.0.0.1:6121/dashboard`:

```bash
pnpm dev:coordinator
```

The existing Dashboard can create a group, register a host, send heartbeats,
inspect group/election state, connect to the loopback-only Local Control API,
control a managed server, validate and transfer artifacts, and handle takeover
actions. Credentials stay in page memory and are cleared on refresh. Follow the
[graphical demo walkthrough](docs/demo.md#graphical-dashboard-demo) for the
bootstrap steps and fake server directory.

## Server-runtime bootstrap

The v0.2 MVP can bootstrap a complete runnable server directory:

```bash
acbh-agent bootstrap create-group \
  --coordinator http://127.0.0.1:6121 \
  --group "example-group" \
  --server-dir /path/to/fabric-a \
  --artifact-class server-runtime
```

Other hosts securely provide `ACBH_ACCESS_KEY`, then run
`bootstrap join-group` to restore and SHA-256 verify the latest
`server-runtime`. Joining does not make the new host current; takeover remains
explicit. See the
[Chinese server-runtime bootstrap guide](docs/zh-CN/server-runtime-bootstrap.md).

## Security defaults

- Local Control binds to `127.0.0.1:6122`; remote binding requires explicit
  opt-in and displays a warning.
- Dashboard secrets are memory-only, masked by default, and cleared on refresh.
- Recommended login and safe-sync commands use `ACBH_ACCESS_KEY` and
  `ACBH_RCON_PASSWORD` instead of placing credentials in argv.
- Manifest fencing, atomic commit, fail-closed GC, restore path hardening, and
  server process locking remain enabled.

Before delivery, follow the
[release readiness checklist](docs/release-checklist.md).

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

Create a group. Save the returned `groupId`, `ownerMemberId`, and one-time `accessKey`.
Use a request file so JSON is not embedded in the curl process arguments:

```bash
printf '%s\n' '{"name":"Survival Server","ownerName":"Owner"}' > group-request.json
curl -s http://localhost:6121/v1/groups \
  -H "content-type: application/json" \
  --data-binary @group-request.json
rm -f group-request.json
```

For join, host registration, heartbeat, and authenticated state requests, use
the Dashboard or the protected header/body-file pattern in
[`scripts/demo-smoke.sh`](scripts/demo-smoke.sh). Do not inline `accessKey`,
`hostToken`, or bearer tokens in curl arguments. Group state responses never
return access keys or host tokens.

## Agent CLI example

The Agent can join the in-memory Coordinator, register the local device, store its local config, and send heartbeats.

Check local diagnostics:

```bash
cd agent
go run . doctor
```

After creating a group with the Coordinator API, log in with the returned one-time access key:

```bash
read -rsp "ACBH access key: " ACBH_ACCESS_KEY
export ACBH_ACCESS_KEY
echo
go run . login \
  --coordinator http://localhost:6121 \
  --group-id <groupId> \
  --name PlayerA \
  --device-name PlayerA-PC \
  --platform windows
unset ACBH_ACCESS_KEY
```

PowerShell users can set `ACBH_ACCESS_KEY` from `Read-Host -AsSecureString`; see
the Windows section in [docs/demo.md](docs/demo.md). The Agent stores config at
`<user config dir>/acbh/config.yaml`. It does not print the host token after
storing it.

Send one heartbeat:

```bash
go run . heartbeat --status standby
```

Run the heartbeat loop:

```bash
go run . daemon --interval 10s --status standby
```

Start the Dashboard-facing local control API:

```bash
go run . control serve
```

The API binds to `127.0.0.1:6122` by default. Its generated bearer token is
stored at `<user config dir>/acbh/control-token` with restrictive permissions;
the full token is not printed. Binding a non-loopback address requires the
explicit `--allow-remote-control` flag and should only be done on a trusted
network. The embedded Dashboard keeps access keys, host tokens, RCON passwords,
and the local control token in page memory only, so they must be entered again
after a refresh.

Then inspect Coordinator state from the Dashboard. Direct API clients should
send host headers from a mode-0600 temporary header file, never as literal
command arguments.

## Agent local server process manager

The Agent can start, stop, and inspect one local server-like process. ACBH does not install Java, Minecraft, server jars, mods, or plugins. The launch command and working directory are supplied by the user and can target Vanilla, Fabric, Forge, NeoForge, Paper, Purpur, Mohist, Arclight, or another command-line server.

The current `config.yaml` file is JSON-encoded. An optional server section looks like:

```json
{
  "server": {
    "dir": "C:/minecraft/server",
    "command": "java -Xmx4G -jar server.jar nogui",
    "logDir": ".acbh/logs",
    "stopTimeout": "30s"
  }
}
```

Existing login fields remain in the same file. CLI flags override these server values:

```bash
go run . server start \
  --server-dir C:/minecraft/server \
  --command "java -Xmx4G -jar server.jar nogui" \
  --log-dir C:/minecraft/server/.acbh/logs \
  --stop-timeout 30s

go run . server status
go run . server stop
go run . server repair-state --server-dir /path/to/server
```

`server start` first acquires an exclusive process lock under `<user config dir>/acbh/runtime`, then launches a detached local supervisor, records runtime state in `server-state.json`, and appends stdout and stderr to separate log files. Concurrent starts fail instead of launching a second Java process. The default log directory is `<user config dir>/acbh/logs`.

`server stop` asks the verified supervisor to write `stop` to the child process stdin, waits for the configured timeout, and then kills the child if it has not exited. Stale or unverifiable state and lock files block new starts by default. `server repair-state` removes them only when all recorded local processes can be confirmed stopped; it never kills a process. Logs are append-only and may grow until the user rotates or removes them.

Launch commands are parsed into an executable and arguments without a shell. Shell operators such as pipes and redirection are not supported. Server process control is local-only; it does not elect a host, perform automatic takeover, send heartbeats, or run artifact synchronization.

### Structured argv (recommended)

The local control API (`POST /v1/server/start`) accepts `jvmArgs` and `serverArgs` as JSON string arrays. The Agent passes these directly to the supervisor as an argv slice, avoiding the vulnerabilities of string join-then-reparse.

### Legacy `--command` string

The CLI `--command` flag accepts a space-separated command string for backward compatibility. It is parsed by `ParseCommand()` which handles quoting and whitespace but cannot recover original intent if an argument contains unquoted spaces (e.g. Windows paths like `C:\Program Files\...`). New configurations should prefer structured argv when possible, and always quote paths containing spaces in `--command`.

The Agent config `server.command` field is a legacy string. It remains supported for backward compatibility with existing `config.yaml` files.

## Agent local manifest examples

The Agent can scan a local Minecraft server directory and generate manifests for one artifact kind at a time. This is local manifest generation only; it does not upload files and it is not RCON safe sync.

Generate a world snapshot manifest. World snapshots include `world-runtime` and `plugin-runtime-data` files. Deleted entries from the previous manifest are written with `deleted: true`, `size: 0`, and an empty `sha256`.

```bash
cd agent
go run . scan \
  --server-dir C:/minecraft/server \
  --artifact-kind world-snapshot \
  --artifact-id snap_000001 \
  --server-pack-version pack_000001 \
  --group-id <groupId> \
  --creator-host-id <hostId> \
  --output ./snap_000001.manifest.json
```

Generate a server pack manifest:

```bash
go run . scan \
  --server-dir C:/minecraft/server \
  --artifact-kind server-pack \
  --artifact-id pack_000001 \
  --group-id <groupId> \
  --creator-host-id <hostId> \
  --output ./pack_000001.manifest.json
```

Validate, inspect, and diff manifests:

```bash
go run . manifest validate --file ./snap_000001.manifest.json
go run . manifest inspect --file ./snap_000001.manifest.json
go run . manifest diff --old ./snap_000001.manifest.json --new ./snap_000002.manifest.json
```

If the Agent config already exists, `scan` can read the group ID and creator host ID from local config. Explicit flags override config values. Unknown and ignored files are counted in the scan report but are never included in manifests.

### RCON safe sync

Plain `scan` reads local files without coordinating with a running Minecraft server. Before generating a `world-snapshot` from a live server, enable RCON in `server.properties`:

```properties
enable-rcon=true
rcon.port=25575
rcon.password=change-me
```

Then run `safe-sync`. It authenticates to RCON, sends `save-all flush`, waits for a successful response, and only then scans the server directory:

```bash
read -rsp "RCON password: " ACBH_RCON_PASSWORD
export ACBH_RCON_PASSWORD
echo
go run . safe-sync \
  --server-dir C:/minecraft/server \
  --artifact-id snap_000001 \
  --server-pack-version pack_000001 \
  --output ./snap_000001.manifest.json \
  --rcon-host 127.0.0.1 \
  --rcon-port 25575
unset ACBH_RCON_PASSWORD
```

The legacy `--rcon-password` flag remains compatible, but the environment
variable avoids exposing the password through shell history and process
arguments. The password is not saved in Agent config or printed.

`safe-sync` only generates a `world-snapshot` manifest. Upload remains a separate step:

```bash
go run . push \
  --manifest ./snap_000001.manifest.json \
  --server-dir C:/minecraft/server
```

Push a scanned manifest and its file objects to the Coordinator local storage backend:

```bash
go run . push \
  --manifest ./snap_000001.manifest.json \
  --server-dir C:/minecraft/server
```

Push streams file objects as `application/octet-stream` by default, so files are not base64-encoded or held entirely in Agent memory. The compatibility JSON/base64 upload can be selected for small test artifacts only:

```bash
go run . push \
  --manifest ./snap_000001.manifest.json \
  --server-dir C:/minecraft/server \
  --legacy-json-upload
```

Pull the latest world snapshot and restore files into a separate directory:

```bash
go run . pull \
  --artifact-kind world-snapshot \
  --artifact-id latest \
  --output-dir ./restore
```

Deleted manifest entries are reported but not applied by default. To remove files listed as deleted entries:

```bash
go run . pull \
  --artifact-kind world-snapshot \
  --artifact-id latest \
  --output-dir ./restore \
  --apply-deletes
```

Object uploads and downloads use binary streaming by default. `ACBH_MAX_OBJECT_BYTES` sets the Coordinator upload limit in bytes and defaults to `268435456` (256 MiB). `POST /v1/artifacts/objects` remains a 16 MiB JSON/base64 compatibility endpoint for testing only. Manifest upload has a 1 MiB request body limit.

Current transfers are not resumable. Interrupted objects must be transferred again; resumable chunks and remote object storage remain future work.

## Election and takeover

ACBH V1 takeover is file restore followed by a new server process start. It is not hot migration. JVM memory, live player sessions, ticks, entities, redstone state, and mod runtime objects are not transferred. Players must reconnect after takeover.

Heartbeats may advertise the artifacts already available on a host, basic score hints, and connection metadata:

```bash
go run . heartbeat \
  --status standby \
  --latest-world-snapshot snap_000001 \
  --latest-server-pack pack_000001 \
  --latest-admin-state admin_000001 \
  --java-available true \
  --connection-host 100.64.0.10 \
  --connection-port 25565 \
  --connection-network tailscale
```

The Coordinator uses a deterministic score and only considers fresh `online` or `standby` hosts. `ACBH_HOST_HEARTBEAT_TIMEOUT_MS` controls freshness and defaults to `30000`. An available latest world snapshot must already be reported locally when one exists, and `javaAvailable: false` makes a host ineligible.

Election and assignment commands:

```bash
go run . election status
go run . election check-timeout

go run . takeover poll
go run . takeover accept
go run . takeover complete
go run . takeover fail --reason pull-failed
```

`takeover poll` stores the one-time takeover token at `<user config dir>/acbh/runtime/takeover-assignment.json` without printing it. The Coordinator stores only its SHA256 hash. Assignment completion, not election, finalizes `currentHostId` and increments `currentHostGeneration`.

Execute an assigned takeover:

```bash
go run . takeover run \
  --server-dir C:/minecraft/server \
  --command "java -Xmx4G -jar server.jar nogui" \
  --log-dir C:/minecraft/server/.acbh/logs \
  --stop-timeout 30s
```

The Agent accepts the assignment, restores assigned artifacts in `server-pack`, `admin-state`, then `world-snapshot` order, applies deleted manifest entries by default, starts the configured local process, sends a `hosting` heartbeat, and completes the assignment. Use `--dry-run` to inspect the plan without accepting, restoring, starting, or completing. The Coordinator never runs Minecraft.

## Local two-host demo

The reproducible fake-server demo is under `examples/two-host-takeover-demo/`:

```powershell
powershell -ExecutionPolicy Bypass -File examples/two-host-takeover-demo/demo.ps1
```

It proves Host A timeout detection, Host B selection, one-time assignment acceptance, streaming artifact restore, fake server start, assignment completion, current-host change, and generation advancement without requiring Minecraft.

## Documentation

- `docs/demo.md` — Local demo walkthrough and troubleshooting
- `docs/security.md` — Security model
- `docs/release-checklist.md` — Pre-release verification checklist
- `docs/architecture.md`
- `docs/dependency-plan.md`
- `docs/mvp-scope.md`
- `docs/sync-design.md`
- `docs/election-design.md`
- `docs/codex-guide.md`

## v0.1-demo

Current release branch: `release/v0.1-demo-prep`

- [Release notes](docs/release-notes-v0.1-demo.md) — completed capabilities, security defaults, known
  limitations, platform verification
- CLI demo: `bash scripts/demo-smoke.sh`
- GUI demo: `pnpm dev:coordinator` → `http://127.0.0.1:6121/dashboard`
- [Single VPS dual-stack deployment guide](docs/zh-CN/deploy-single-vps-dual-stack.md)
  — two Velocity/Fabric entries with a Dashboard-assisted takeover walkthrough
- [v0.2 real VPS runbook](docs/zh-CN/v0.2-real-vps-runbook.md)
- [server-runtime host bootstrap](docs/zh-CN/server-runtime-bootstrap.md)
  — deploy and verify the dual-stack flow on a real low-cost public VPS
- [Release packaging](docs/release-packaging.md) — how to build `dist/release/v0.1-demo`
  artifacts for Linux and Windows
- [Release checklist](docs/release-checklist.md) — 9-section pre-release verification
- Go tests: 14 packages (all pass), Coordinator tests: 123/123
- Verified on Fedora 41 and Windows 11 (PowerShell)

**Not production ready.** Loopback-only, in-memory by default, no TLS.
Plain-text HTTP on localhost is acceptable for local development only.

## V1 release

- `docs/v1-release-checklist.md` — Smoke checklist for V1 release preparation
- `docs/release-checklist.md` — Release readiness checklist
- `docs/v1-release-notes.md` — V1 capabilities, security, limitations, next milestones
- `docs/release-packaging.md` — How to build distributable Agent binaries
- `docs/tunnel-protocol.md` — Relay and player proxy protocol details
- `docs/network-design.md` — Network and relay architecture
