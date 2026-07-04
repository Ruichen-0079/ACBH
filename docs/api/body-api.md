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
- `GET /v1/relay/status`

Listener APIs inspect the configured local Minecraft TCP port and process metadata. They do not start, stop, kill, repair, or supervise the Minecraft server.

Relay APIs configure runtime coordinator state for the current host by ensuring the host lease and sending a heartbeat with the configured local endpoint. They do not perform backup, snapshot, restore, or byte-stream tunnel work in this phase.

## Implemented in Phase 2.5

Body exposes user-facing identity as `identityModel: "single-owner"`. `GET /v1/config` returns schemaVersion 2 with `instance`, `device`, `server`, and `compat`; token fields are redacted in HTTP responses. Runtime code still keeps the full token in `%APPDATA%/ACBH/config.json`.

`GET /v1/identity` returns the private instance, current device, current server, Coordinator protocol, and compatibility booleans. It does not return full legacy tokens.

Operations return `operationId`, `traceId`, `state`, `stage`, `progress`, `startedAt`, `completedAt`, `errorCode`, and `message`.

Errors include `errorCode`, `message`, `details.url`, `details.method`, `details.httpStatus`, `details.responseBody`, `details.configPath`, and `details.coordinatorUrl` when available.
