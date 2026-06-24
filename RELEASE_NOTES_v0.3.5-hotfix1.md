# ACBH v0.3.5 Hotfix 1

This hotfix is based on the first end-to-end VPS deployment of v0.3.4.

## Included

- Correct VPS installer environment names for Coordinator and public relay ports.
- Fix public relay group discovery and prefer the group that currently has an active host.
- Add a Windows hotfix panel that shows Group ID, Member ID, Host ID, Coordinator URL, current host, Minecraft state, and relay state.
- Add visible invite generation and invite history actions.
- Add visible actions to pull the latest server pack and world snapshot from the public VPS.
- Add current-host repair and explicit relay-start actions.
- Add `scripts/build-agent-release-v035.sh`, which packages the hotfix panel as the default desktop panel and preserves the previous panel as `acbh-desktop-gui-legacy.ps1`.

## Build

```bash
VERSION=v0.3.5-hotfix1 ./scripts/build-agent-release-v035.sh
```

For repository testing without rebuilding the desktop executable, run the panel directly from the bundle root:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -STA -File .\scripts\acbh-desktop-gui-v035.ps1 -AgentPath .\acbh-agent-windows-amd64.exe
```

## Safe defaults

- Pulling the latest server pack does not apply remote deletion records to local-only files.
- Invite codes are copied to the clipboard when generated.
- Access keys and host tokens are never rendered in the panel.

## Known v0.3.4 issues addressed

- Installer variables did not match the variables consumed by the Coordinator runtime.
- Public ingress could return `No groups configured` even after a group had been created.
- Public ingress selected the first persisted group instead of preferring the active group.
- Critical identity and synchronization actions were hidden in the advanced panel.

## Remaining follow-up

The existing v0.3.4 `start-auto` and status implementations still need a deeper transaction-state cleanup so they cannot report `Running` before current-host and relay verification complete. The hotfix panel performs explicit current-host and relay checks around those commands.
