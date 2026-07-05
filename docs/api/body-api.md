# Body API

Default endpoint: `http://127.0.0.1:6120`.

## Implemented in Stage 1

- `GET /v1/body/health`
- `GET /v1/config`
- `PUT /v1/config`
- `GET /v1/identity`
- `GET /v1/coordinator/probe`
- `POST /v1/init`
- `GET /v1/operations`
- `GET /v1/operations/:operationId`

## Implemented in Phase 2

- `GET /v1/listener/status`
- `PUT /v1/listener/config`
- `POST /v1/listener/probe`
- `POST /v1/relay/configure`
- `POST /v1/relay/start`
- `POST /v1/relay/stop`
- `GET /v1/relay/status`

Listener APIs inspect the configured local Minecraft TCP port and process metadata. They do not start, stop, kill, repair, or supervise the Minecraft server.

Relay APIs configure runtime coordinator state for the current host by ensuring the host lease and sending a heartbeat with the configured local endpoint. Phase 2.6 adds the actual public TCP tunnel: players connect to the VPS public port, Coordinator creates relay tunnel sessions, the agent opens outbound host WebSockets, and byte streams are forwarded to the configured local Minecraft endpoint.

`GET /v1/relay/status` reports `configured`, `localServerListening`, `tunnelConnected`, `publicListenerActive`, `publicEndpoint`, `localEndpoint`, `activeConnections`, and `lastError`. Relay APIs do not perform backup, snapshot, restore, or Minecraft server lifecycle work.

## Implemented in Phase 2.5

Body exposes user-facing identity as `identityModel: "single-owner"`. `GET /v1/config` returns schemaVersion 2 with `instance`, `device`, `server`, and `compat`; token fields are redacted in HTTP responses. Runtime code still keeps the full token in `%APPDATA%/ACBH/config.json`.

`GET /v1/identity` returns the private instance, current device, current server, Coordinator protocol, and compatibility booleans. It does not return full legacy tokens.

Operations return `operationId`, `traceId`, `state`, `stage`, `progress`, `startedAt`, `completedAt`, `errorCode`, and `message`.

Errors include `errorCode`, `message`, `details.url`, `details.method`, `details.httpStatus`, `details.responseBody`, `details.configPath`, and `details.coordinatorUrl` when available.

## Implemented in Phase 3

- `POST /v1/backup/analyze`
- `POST /v1/backup/upload`
- `GET /v1/snapshots`
- `POST /v1/snapshots/latest/download`
- `POST /v1/snapshots/:snapshotId/download`

Backup analysis scans the configured `server.dir` without requiring a Minecraft listener. The default profile includes migratable server data such as `world`, `mods`, `config`, `defaultconfigs`, datapack/resource directories, and top-level files such as `server.properties`, `eula.txt`, `ops.json`, `whitelist.json`, `banned-ips.json`, and `banned-players.json`. It excludes runtime/install data such as `libraries`, `jre`, `logs`, `crash-reports`, `versions`, and caches.

Backup upload runs through the body API and Coordinator protocol v2 world-backup routes. It ensures the current host lease, queries missing objects, uploads missing content-addressed objects, and commits a snapshot manifest. It does not expose group/member/host token fields in user-facing body responses.

Snapshot download requires `targetDir`. The target directory is created if missing, but an existing non-empty directory is rejected unless `allowNonEmpty=true`. Restore paths are normalized and checked to stay inside `targetDir`; symlink and Windows reparse-point/junction targets are blocked. Top-level snapshot files are valid and restore directly under `targetDir`.

Coordinator route inventory used internally by Phase 3:

- `POST /v1/groups/:groupId/world-backups/plan`
- `PUT /v1/groups/:groupId/world-objects/:sha256`
- `POST /v1/groups/:groupId/world-backups/commit`
- `GET /v1/groups/:groupId/world-backups`
- `GET /v1/groups/:groupId/world-backups/latest`
- `GET /v1/groups/:groupId/world-backups/:snapshotId`
- `GET /v1/groups/:groupId/world-objects/:sha256`
