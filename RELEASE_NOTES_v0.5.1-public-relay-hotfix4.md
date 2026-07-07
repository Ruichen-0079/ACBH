# ACBH v0.5.1-public-relay-hotfix4

Public relay and Windows GUI hotfix release.

## Fixes

- Fix public relay downstream race where fast host/Minecraft responses could be emitted before the public TCP ingress registered its WebSocket message handler.
- Preserve relay pair `groupId` when public ingress creates a pair before host/player attach, so tunnel sessions can transition to `active` and `closed` reliably.
- Require real public Minecraft status ping success before the GUI/Body relay state reports `active`.
- Poll tunnel sessions immediately when relay runtime starts, reducing first-connect race windows.
- Fix minimal-core GUI text clipping and overlap in the first-use steps, listener/relay labels, and status/log layout.
- Add GUI self-test checks for visible text width and sibling control overlap to prevent regressions.
- Align Body and Coordinator reported versions with this release.

## Verify

```text
go test ./... -count=1 -timeout=10m
node --test --import tsx test/api.test.ts test/election.test.ts test/storage.test.ts test/world-backups.test.ts test/health-version.test.ts test/capabilities-lease.test.ts test/token-only-relay.test.ts test/gc.test.ts test/persistence.test.ts test/tunnel.test.ts test/relay.test.ts test/public-relay.test.ts test/dashboard.test.ts test/auth.test.ts test/invite-security.test.ts test/fixture.test.ts test/contract.test.ts test/package-json.test.ts
node_modules\.bin\tsc.CMD -p tsconfig.json --noEmit
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts\acbh-minimal-core-gui.ps1 -SelfTest
```

## Deploy

1. Upload and extract the matching Coordinator bundle on the VPS.
2. Restart the Coordinator with the same `ACBH_ACCESS_TOKEN`.
3. Use the matching Windows zip on the host machine.
4. Confirm `/version`, Body health, and `build-info.json` all report this release version and commit.
