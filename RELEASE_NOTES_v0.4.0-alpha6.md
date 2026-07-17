# v0.4.0-alpha6

Alpha6 hotfix for alpha3 desktop user-reported issues after PR #65 merge, plus PR #66 path picker and backup default fixes.

Highlights:

- Coordinator capability negotiation before lease operations: probes `/health` and `/v1/capabilities`, degrades safely on old VPS coordinators.
- Bootstrap and operation envelopes now carry explicit `outcome`; `ok` is always boolean; warnings are plain text (no nested failure JSON).
- Windows native file pickers via `POST /api/picker/{folder,file,files}` for server directory, launch file, and Java executable.
- Desktop GUI keeps paths in JSON bodies only; `Idempotency-Key` and `requestId` are ASCII-only (fixes Chinese path `non ISO-8859-1` fetch errors).
- `PUT /api/config/server` validates paths, saves with read-back verification, and auto-generates backup profiles under the Minecraft server directory.
- `GET /api/backup/summary` falls back to `desktop-config.json` when agent `config.yaml` is not yet created.
- Group create/join state machine with duplicate-operation guards, `reset-local`, and `GET /api/group/members`.
- Backup upload and lease-dependent actions are disabled when `lease_renew_v1` is unavailable, with Chinese disable reasons in the GUI.
- Coordinator adds `GET /v1/groups/:groupId/members` and advertises `capabilities_v1` / `group_whoami_v1`.

Upgrade notes:

- Desktop Windows users: download `acbh-v0.4.0-alpha6-bundle.zip`, extract, and run `acbh-desktop-windows-amd64.exe`.
- VPS coordinators should be upgraded with `acbh-coordinator-linux-amd64-bundle.tar.gz` before using member list and full lease capabilities from this desktop build.

Known limits:

- Old VPS coordinators without `/v1/capabilities` show degraded bootstrap warnings instead of fatal `operation_failed` errors; backup upload stays disabled until VPS is upgraded.