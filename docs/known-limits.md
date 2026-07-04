# Known Limits

v0.5 minimal-core currently implements config, local body/runtime, Coordinator probe/init, local listener detection, relay state configuration, backup upload, snapshot listing/download, and the minimal GUI path for those flows.

Not yet on the v0.5 main path:

- diagnostic bundle export
- Windows release artifact assembly
- Coordinator artifact repackaging and `SHA256SUMS`
- actual relay byte-stream tunneling from players through the public node
- Minecraft server lifecycle control from the GUI

Compatibility limits:

- Coordinator protocol v2 still uses legacy `/v1/groups/:groupId/...` routes internally.
- Users do not need to understand group/member terminology in v0.5 normal flows.
- Protocol v3 can remove the legacy group API after existing VPS data and object namespaces are migrated.
