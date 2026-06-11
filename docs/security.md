# Security Model

ACBH V1 uses a minimal security model focused on preventing unauthorized hosts and invalid snapshots.

## Roles

- `owner`: manages group and recovery.
- `admin`: manages members and host candidates.
- `host_candidate`: may be elected as host.
- `member`: may view group connection metadata.
- `guest`: no Agent privileges.

## Access Key

An Access Key allows a player to join a group.

Rules:

- Do not store raw access keys.
- Store a hash.
- Allow rotation.
- Do not log secrets.

## Host Token

A Host Token is unique to one Agent installation/device.

Required for:

- host registration;
- heartbeat;
- snapshot upload;
- takeover execution.

Rules:

- Do not store raw host tokens.
- Store a hash.
- Allow revocation.
- Token must be scoped to one group and one host.

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

RCON passwords are runtime-only Agent inputs. `safe-sync` accepts the password from `--rcon-password` or `ACBH_RCON_PASSWORD`; it does not write the password to Agent config, manifests, Coordinator storage, or command output.

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
