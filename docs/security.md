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

A host that was previously current but timed out must not overwrite the new current host after reconnecting.

Rules:

- Every snapshot upload should include host ID and expected current-host generation.
- Coordinator should reject uploads from stale hosts unless manually accepted.
- Takeover should be explicit and single-winner.

## Secrets

Never print these values in logs:

- Access Key
- Host Token
- RCON password
- storage credentials
