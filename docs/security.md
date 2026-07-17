# Security Model

ACBH V1 uses a minimal security model focused on preventing unauthorized hosts and invalid snapshots.

## Roles

- `owner`: manages group and recovery (created at group creation; one per group).
- `member`: joined the group via access key. May register hosts. Not used for any elevated auth check beyond group membership validation.
- `host`: a registered Agent that may heartbeat, upload artifacts, participate in elections, execute takeover. Requires a `hostToken`.
- `player`: an external peer that may create tunnel sessions for relay connections. Requires a `playerToken`, scoped to one group.
- `guest`: no Agent privileges (public endpoints only).

Role boundaries:
- A host token is valid only for the group it was registered in. Cross-group use returns 404.
- A player token cannot call host API endpoints (artifact, election, takeover) and returns 401.
- A host token cannot call player tunnel endpoints and returns 401.
- Tokens are rejected when `expiresAt <= now`. Expiry returns 401 with code `token_expired`.
- Error responses never contain raw token values.

## Access Key

An Access Key allows a player to join a group.

Rules:

- Do not store raw access keys.
- Store a hash.
- Allow rotation.
- Do not log secrets.

## Host Token

A Host Token is unique to one Agent installation/device.

An existing group Access Key is required to register a host. The registration
response returns the new Host Token once and never echoes the Access Key.

A Host Token is required for:

- heartbeat;
- snapshot upload;
- takeover execution.

Rules:

- Do not store raw host tokens.
- Store a hash.
- Allow revocation.
- Token must be scoped to one group and one host.

## Group state access

Group state and debug state endpoints are not public. They require either:

- the group's Access Key via `x-acbh-access-key`; or
- a valid Host ID and Host Token for the same group.

Responses must never include Access Keys, Host Tokens, or their hashes.

## Artifact push and pull

Object and manifest upload endpoints require `groupId`, `hostId`, and `hostToken`. Artifact list, latest metadata, manifest download, and object download also require host authentication. The Coordinator verifies the host token and never returns it.

## Snapshot verification

Uploaded snapshots must not become latest until verified.

Coordinator must check:

- manifest is valid JSON;
- all file paths are normalized and safe;
- no path traversal is allowed;
- object SHA256 matches manifest;
- required metadata is present;
- uploader has host permission.

## Stale host protection

Only the current host may publish artifacts that become available or advance the latest pointer. SHA256 object blob uploads remain unrestricted so standby hosts can pre-warm storage.

Rules:

- Manifest upload and artifact recording must include host identity (hostId, hostToken) verified by the Coordinator.
- When `group.currentHostId` is null (initial state before any takeover completes), any authenticated host may publish without a generation header.
- When `group.currentHostId` is set, the `X-ACBH-Host-Generation` header is required. Missing or malformed headers return 400.
- When `group.currentHostId` is set, only the current host may publish. Uploads from other hosts return 403.
- A generation mismatch returns 409, signaling that the current host changed.
- Stale hosts cannot overwrite the latest snapshot through artifact publish. On rejection, the latest pointer is unchanged.
- Standby hosts may still upload raw SHA256 objects to the storage backend for future snapshot assembly.
- Takeover should be explicit and single-winner.

## Takeover token and generation

Election APIs and takeover APIs require a valid group-scoped Host Token. The assigned host receives a separate short-lived takeover token on its first poll.

Rules:

- Store only the takeover token SHA256 hash in Coordinator assignment state.
- Return the raw takeover token once and only to the assigned host.
- Store the raw takeover token only in the assigned Agent runtime directory with user-only file permissions.
- Never print either Host Token or takeover token.
- Reject expired, cancelled, failed, completed, or stale-generation assignments.
- Finalize `currentHostId` and increment `currentHostGeneration` only after successful completion.
- Cancel the previous active assignment when a new election runs.

## Artifact garbage collection

The `POST /v1/groups/:groupId/artifacts/gc` endpoint allows the current host to remove old artifact manifests and unreferenced object blobs. GC can be triggered manually via `acbh-agent gc` or the Coordinator API.

Rules:

- Header-based auth: `X-ACBH-Host-ID`, `X-ACBH-Host-Token`, and `X-ACBH-Host-Generation` (when a current host exists).
- Only the current host may run GC when `currentHostId` is set. Other hosts receive 403.
- A stale generation header (not matching `currentHostGeneration`) returns 409.
- Missing or invalid generation header returns 400.
- `dryRun` defaults to `true`. Dry runs never mutate store or storage.
- GC does not run automatically. It must be triggered manually.

Protected artifacts cannot be deleted:
- The latest artifact per kind
- Artifacts referenced by an active takeover assignment
- The N most recent `available` artifacts per kind
- Artifacts younger than the configured minimum age
- Artifacts with `uploading` status

Only `available` and `rejected` artifacts are deletion candidates. Object blobs are only deleted when no retained manifest references them.

## Coordinator state persistence

The Coordinator persists its in-memory state to a local JSON file so group, host, artifact, election, and takeover metadata survive restarts.

Rules:

- State is saved to `ACBH_COORDINATOR_STATE_PATH`. Set this env var to a file path (e.g. `./data/coordinator-state.json`) to enable persistence. Unset or empty disables it.
- The file contains host token hashes and takeover token hashes, never the raw secrets.
- Store only the SHA256 hash of host tokens and takeover tokens in persisted state. Raw Host Token and Takeover Token are returned at registration/poll time and never written to the file.
- The state file should be protected with restrictive file permissions (operator responsibility).
- Writes use a temp-file + rename strategy to avoid corrupting the file on crash.
- A `version` field guards against loading future or invalid formats.

## Secrets

Never print these values in logs:

- Access Key
- Host Token
- RCON password
- Takeover Token
- storage credentials

The recommended Agent login path reads the group access key from
`ACBH_ACCESS_KEY`. The legacy `--access-key` flag remains compatible, but
Dashboard-generated commands and demo documentation do not put the key in
process arguments.

RCON passwords are runtime-only Agent inputs. The recommended `safe-sync` path
uses `ACBH_RCON_PASSWORD`. The legacy `--rcon-password` flag remains compatible.
The password is not written to Agent config, manifests, Coordinator storage, or
command output.

## Player tunnel and session security

Player-to-host tunneling uses ephemeral player session tokens and host
authentication for relay connections (PR27).

Rules:

- Player session creation (`POST /v1/groups/:groupId/player-sessions`) returns
  a one-time raw player token. The Coordinator stores only the SHA256 hash.
- `getPlayerSession` and all tunnel session endpoints must never return the
  raw player token.
- Player relay WebSocket connections require `X-ACBH-Player-ID` and
  `X-ACBH-Player-Token` headers. Invalid tokens are rejected.
- Player tokens are rejected when `expiresAt <= now`. Expired credentials
  return `401` with code `token_expired`, without returning the raw token.
- Tunnel session create and read endpoints require matching
  `X-ACBH-Player-ID` and `X-ACBH-Player-Token` headers.
- Host relay WebSocket connections require `X-ACBH-Host-ID`,
  `X-ACBH-Host-Token`, and `X-ACBH-Host-Generation` headers.
- Host tunnel presence must never expose the local Minecraft address to
  players.
- The relay forwards opaque binary frames. Relay payloads must never be
  logged or stored.
- Relay runtime state is ephemeral and not persisted across Coordinator
  restarts.
- Production deployment should use HTTPS/WSS for transport security.
- No host token, takeover token, or player token material is exposed through
  tunnel session responses or relay endpoints.

## Local control and Dashboard secrets

The Agent local control API is a privileged local interface.

- `acbh-agent control serve` binds to `127.0.0.1:6122` by default.
- Non-loopback binding is refused unless `--allow-remote-control` is supplied.
  The Agent logs a warning when that explicit override is used.
- All operations except `/health` require the same bearer-token middleware.
- The generated token is stored in `<user config dir>/acbh/control-token` with
  restrictive permissions. Logs and normal command output show only a masked
  token.
- Browser CORS access is limited to `localhost`, `127.0.0.1`, and `::1`
  origins by default.
- Operation failures return a stable error code and request ID. Detailed
  errors, including local paths, remain in the Agent's local log.
- Manifest validation is exposed through the same authenticated Local Control
  middleware and returns a generic error plus request ID on failure.
- The Dashboard never stores access keys, host tokens, takeover tokens, player
  tokens, RCON passwords, or the local control token in `localStorage`,
  `sessionStorage`, IndexedDB, URLs, or console logs. It removes legacy secret
  keys at startup and keeps current credentials in page memory only.
- A `401` or `403` Local Control response clears the in-memory control token.
- Non-loopback Local Control URLs display a warning and require an explicit
  confirmation before connection.
- Dashboard command generators use `ACBH_ACCESS_KEY` and
  `ACBH_RCON_PASSWORD`; they do not interpolate credential values into argv.

## Host Agent relay client security (PR28)

The Host Agent relay tunnel client (`acbh-agent relay host`) enforces:

- The host's local Minecraft address (`--target-address`, typically
  `127.0.0.1:25565`) is a local-only configuration on the Host Agent. It is
  never transmitted to the Coordinator or exposed to players.
- The host token is sent only via WebSocket upgrade headers to the
  Coordinator. It is never logged, printed to stdout/stderr, or included
  in error messages.
- Binary payloads are forwarded opaque between the WebSocket and TCP
  connections. No payload bytes are logged or inspected.
- The relay client does not parse or interpret the Minecraft protocol.
- Context cancellation triggers clean shutdown of both WebSocket and TCP
  connections.

## Player local proxy security (PR29)

The Player local TCP proxy (`acbh-agent relay player`) enforces:

- The local proxy binds to `127.0.0.1` by default, limiting access to the
  local machine.
- If `--listen-address` is explicitly set to `0.0.0.0`, the proxy binds to
  all interfaces and may be reachable from other LAN hosts. This should be
  used carefully and only in trusted network environments.
- The player token is never logged, printed to stdout/stderr, or included
  in error messages.
- Binary payloads are forwarded opaque between TCP and WebSocket without
  logging or inspection.
- The proxy does not parse the Minecraft protocol.
- Context cancellation triggers clean shutdown of listener, local TCP
  connections, and WebSocket connections.

## Manifest schema Go/TypeScript unification

Manifest validation is unified across Go (Agent scanner/loader) and TypeScript
(Coordinator storage). Shared test fixtures ensure both sides enforce the same
constraints.

Common rules:

- `manifestVersion` is optional. When present it must equal `1`.
- `artifactKind` must be one of `server-pack`, `world-snapshot`, `admin-state`.
- `artifactId`, `groupId`, `creatorHostId` are required non-blank identifiers
  matching `[A-Za-z0-9][A-Za-z0-9_.-]{0,127}`.
- `createdAt` is a required valid ISO 8601 timestamp.
- `serverPackVersion` is required for `world-snapshot` manifests.
- `parentArtifactId` is `null` or a valid identifier.
- `files` must be an array sorted by path with no duplicate entries.
- Each file entry requires `class` (one of the six `FileClass` values;
  `ignored` and `unknown` are rejected). The legacy `fileClass` key is
  normalized to `class` when only `fileClass` is present.
- `size` must be a non-negative safe integer.
- `sha256` must be a 64-character lowercase hex string for non-deleted files.
  Deleted tombstone entries must set `sha256: ""` and `size: 0`.
- `modifiedAt` is required for non-deleted files.
- File paths must be relative POSIX paths, cannot contain `\`, and must not
  traverse directories.
- `summary` fields (`includedFiles`, `deletedFiles`, `totalBytes`) must match
  the file list.

## Server start command and argv

The server start command avoids shell interpretation. The local control API
and the internal supervisor use structured `argv` (`[]string`):

- The local control API endpoint receives `jvmArgs` and `serverArgs` as JSON
  string arrays. The Agent builds the `java <jvmArgs> -jar <jarPath>
  <serverArgs>` command as an argv slice and passes it to the supervisor
  without string join-then-reparse.
- The CLI `--command` flag accepts a legacy space-separated string and is
  parsed by `ParseCommand()` for backward compatibility. Structured argv is
  preferred for new configurations.
- The supervisor launches the Minecraft process via `exec.Command` directly,
  never through a shell.
- Paths containing spaces work correctly with structured argv.
- Shell metacharacters are never interpreted.
