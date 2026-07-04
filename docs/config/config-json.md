# config.json

The v0.5 authoritative config is `%APPDATA%/ACBH/config.json`.

Rules:

- JSON only.
- Do not use YAML-style keys such as `"coordinatorUrl: http://x.x.x.x:6121"`.
- `remote-public` mode rejects localhost Coordinator URLs.
- `groupId`, `hostId`, and `hostToken` are never silently regenerated.
- Writes use a temp file, fsync, and rename.
- Broken JSON is renamed to `config.json.broken-<timestamp>`.

Minimal remote-public config:

```json
{
  "schemaVersion": 1,
  "mode": "remote-public",
  "coordinatorUrl": "http://121.40.101.224:6121",
  "identity": {
    "groupId": "grp_xxx",
    "memberId": "mem_xxx",
    "hostId": "host_xxx",
    "hostToken": "ht_xxx",
    "displayName": "私人本地主机",
    "deviceName": "MSI",
    "platform": "windows"
  }
}
```
