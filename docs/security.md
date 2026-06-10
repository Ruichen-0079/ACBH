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

The current generation is a control-plane guard, not full distributed fencing. Snapshot upload generation enforcement and durable consensus remain future work.

## Secrets

Never print these values in logs:

- Access Key
- Host Token
- RCON password
- Takeover Token
- storage credentials

RCON passwords are runtime-only Agent inputs. `safe-sync` accepts the password from `--rcon-password` or `ACBH_RCON_PASSWORD`; it does not write the password to Agent config, manifests, Coordinator storage, or command output.
