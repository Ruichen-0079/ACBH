# config.json

The v0.5 authoritative config is `%APPDATA%/ACBH/config.json`.

Rules:

- JSON only.
- Do not use YAML-style keys such as `"coordinatorUrl: http://x.x.x.x:6121"`.
- `remote-public` mode rejects localhost Coordinator URLs.
- Legacy Coordinator IDs and tokens are never silently regenerated.
- Writes use a temp file, fsync, and rename.
- Broken JSON is renamed to `config.json.broken-<timestamp>`.

Minimal remote-public config:

```json
{
  "schemaVersion": 2,
  "mode": "remote-public",
  "coordinatorUrl": "http://121.40.101.224:6121",
  "instance": {
    "instanceId": "inst_xxx",
    "displayName": "私人 ACBH 实例",
    "ownerToken": "ht_xxx"
  },
  "device": {
    "deviceId": "dev_xxx",
    "displayName": "MSI",
    "platform": "windows"
  },
  "server": {
    "serverId": "srv_xxx",
    "displayName": "弥散往生1.2.4",
    "dir": "C:\\Users\\21982\\Desktop\\server"
  },
  "compat": {
    "coordinatorProtocol": 2,
    "legacyGroupId": "grp_xxx",
    "legacyMemberId": "mem_xxx",
    "legacyHostId": "host_xxx",
    "legacyHostToken": "ht_xxx"
  },
  "listener": {
    "enabled": true,
    "localHost": "127.0.0.1",
    "localPort": 25565
  },
  "relay": {
    "enabled": true,
    "publicHost": "121.40.101.224",
    "coordinatorPort": 6121,
    "minecraftPort": 25565
  },
  "backup": {
    "profileId": "minecraft-migratable",
    "include": [
      "dir:world",
      "dir:mods",
      "dir:config",
      "file:server.properties",
      "file:banned-ips.json"
    ],
    "exclude": [
      "dir:libraries",
      "dir:jre",
      "dir:logs",
      "dir:crash-reports",
      "dir:versions"
    ]
  }
}
```

Field notes:

- `instance` is the user-visible private ACBH instance.
- `device` is the current Windows machine.
- `server` is the current Minecraft server configuration.
- `ownerToken` is the user-facing access token name.
- `compat` exists only because Coordinator protocol v2 still uses legacy group routes. GUI normal views hide full legacy values and redact tokens.
- `backup` controls the minimal Phase 3 backup profile. Empty `include` or `exclude` lists are filled with the built-in Minecraft migratable defaults; custom non-empty lists are preserved.
