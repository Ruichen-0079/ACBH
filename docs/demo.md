# ACBH Demo

This guide runs ACBH locally without a real Minecraft server, public network,
domain, tunnel, or cloud service.

## Prerequisites

| Tool | Minimum | Purpose |
|------|---------|---------|
| Go | 1.22 | Agent build and tests |
| Node.js | 20 | Coordinator runtime |
| pnpm | 9 | Workspace package manager |
| curl | any | CLI smoke HTTP checks |

### Fedora

```bash
sudo dnf install golang nodejs curl
corepack enable
corepack pnpm install
```

### Windows

Install Go and Node.js, open a new PowerShell window, then run:

```powershell
corepack enable
corepack pnpm install
```

## Verify everything

Fedora, macOS, or Git Bash:

```bash
bash scripts/verify-all.sh
```

Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/verify-all.ps1
```

The Coordinator suite should report at least 123 passing tests.

## CLI demo smoke

```bash
bash scripts/demo-smoke.sh
```

The script builds Coordinator and Agent, starts Coordinator on a temporary
loopback port, creates a group and host, sends a heartbeat, scans and validates
a fake manifest, pushes and pulls an artifact, verifies the restored file,
checks authenticated group state, and cleans up.

It uses a temporary `XDG_CONFIG_HOME` and Windows `APPDATA`. Curl request bodies
and authentication headers are written to mode-restricted temporary files, not
literal process arguments. The cleanup trap stops Coordinator and removes all
temporary config, storage, logs, and restored files.

## Graphical Dashboard demo

The existing Dashboard is served by Coordinator at
`http://127.0.0.1:6121/dashboard`. Normal control actions are clickable. The
command line is only needed to start local services and bootstrap Agent config.

### 1. Start Coordinator

Fedora:

```bash
corepack pnpm dev:coordinator
```

Windows PowerShell:

```powershell
corepack pnpm dev:coordinator
```

Open `http://127.0.0.1:6121/dashboard`. Confirm that Coordinator is online and
Storage is ready.

### 2. Create a group and exercise host registration

1. Open **Coordinator**.
2. Click **Create group**.
3. Confirm that `groupId` and owner `memberId` are populated.
4. The access key is masked and held in page memory only.
5. Set device name and platform, then click **Register host**.
6. Click **Send heartbeat** and **Load group state**.

The state view shows current host, generation, hosts, score hints, and heartbeat
timestamps without returning access keys or host tokens.

### 3. Bootstrap the managed Agent host

Local Control push and pull use the Agent config. Run login once with the access
key in an environment variable. The key is not placed in argv.

Fedora:

```bash
read -rsp "ACBH access key: " ACBH_ACCESS_KEY
export ACBH_ACCESS_KEY
echo
cd agent
go run . login \
  --coordinator http://127.0.0.1:6121 \
  --group-id <groupId> \
  --name "Demo Host" \
  --device-name demo-fedora \
  --platform linux
unset ACBH_ACCESS_KEY
```

Windows PowerShell:

```powershell
$secret = Read-Host "ACBH access key" -AsSecureString
$env:ACBH_ACCESS_KEY = [System.Net.NetworkCredential]::new("", $secret).Password
Set-Location agent
go run . login --coordinator http://127.0.0.1:6121 --group-id <groupId> --name "Demo Host" --device-name demo-windows --platform windows
Remove-Item Env:ACBH_ACCESS_KEY
```

Login writes the managed host ID and host token to
`<user config dir>/acbh/config.yaml` with restrictive permissions. Enter those
values in the Dashboard when using host-authenticated controls. They remain in
page memory and are cleared by refresh or **Clear credentials**.

### 4. Start and connect Local Control

From `agent/`:

```bash
go run . control serve
```

Read the full token from `<user config dir>/acbh/control-token`, enter it in the
Dashboard, and click **Connect local Agent**. The default URL is
`http://127.0.0.1:6122`.

Local Control is loopback-only by default. A non-loopback URL displays a
prominent warning and requires an extra confirmation. Do not expose it to the
public Internet.

### 5. Create a fake server directory

Fedora:

```bash
mkdir -p /tmp/acbh-gui-demo/world/region
printf 'world-data\n' > /tmp/acbh-gui-demo/world/region/r.0.0.mca
printf 'motd=ACBH demo\n' > /tmp/acbh-gui-demo/server.properties
```

Windows PowerShell:

```powershell
$demo = Join-Path $env:TEMP "acbh-gui-demo"
New-Item -ItemType Directory -Force (Join-Path $demo "world\region") | Out-Null
Set-Content (Join-Path $demo "world\region\r.0.0.mca") "world-data"
Set-Content (Join-Path $demo "server.properties") "motd=ACBH demo"
```

Set **Server A** to that directory and **Server B** to a separate restore
directory.

### 6. Use the graphical controls

In **Agent**:

1. Click **View status**. No managed server should be running.
2. Click **Scan server-pack**.
3. Set the generated manifest path and click **Validate manifest**.
4. Click **push server-pack**.
5. Open **Artifacts** and click **Latest artifact**.
6. Click **pull server-pack** and confirm the restore.

`Start server` and `safe-sync world` require a real Java server and RCON
endpoint. The fake-directory demo intentionally uses only status, manifest, and
artifact operations.

In **Election / Takeover**:

1. Click **Refresh election status**.
2. Use **Run election** or **Check timeout** only for fault-takeover testing.
3. Poll, accept, complete, or fail an assignment with the confirmation dialogs.

These actions are fault takeover, not an ordinary restart. The **Events** tab
shows recent page-local status summaries after redacting current credential
values.

## Security defaults

- Local Control binds to `127.0.0.1:6122`.
- Remote binding requires `--allow-remote-control` and prints a warning.
- Access keys, host tokens, Local Control tokens, takeover tokens, player
  tokens, and RCON passwords are never stored in `localStorage`,
  `sessionStorage`, IndexedDB, URL query strings, fragments, or console logs.
- Secret inputs are masked and only reveal temporarily after an explicit click.
- A page refresh clears credentials.
- Stop, restore, and takeover actions require confirmation.
- Server start uses structured `jvmArgs` and `serverArgs`.
- Legacy `server.command` and `--command` remain compatibility paths.

## Agent config

The template is [`agent/config.example.json`](../agent/config.example.json).
Use placeholders only in documentation and commit history. On Fedora, protect a
real config with:

```bash
chmod 600 ~/.config/acbh/config.yaml
chmod 600 ~/.config/acbh/control-token
```

On Windows, the files are under `%AppData%\acbh`.

## Common issues

### Port occupied

Coordinator:

```text
Error: listen EADDRINUSE
```

Choose another port:

```bash
PORT=6123 corepack pnpm dev:coordinator
```

Local Control reports its own listen failure. Stop the process using port 6122
or pass another loopback address such as `--listen 127.0.0.1:6131`, then update
the Dashboard URL.

### Local Control token rejected

Read the complete token from the token file. A 401 or 403 response clears the
Dashboard token field and requires it to be entered again.

### Remote Local Control rejected

```text
control: refusing non-loopback listen address
```

This is the safe default. Use `--allow-remote-control` only on a trusted network
after reviewing [security.md](security.md).

### Manifest validation failed

Use `acbh-agent scan` or the Dashboard scan button to generate a manifest.
Validation enforces sorted files, safe paths, hashes, classes, artifact-kind
compatibility, and summary counts.

### Windows path or command issue

Prefer the Dashboard structured server start fields. The legacy `--command`
string requires quoting paths with spaces and cannot recover incorrectly split
arguments.

### Coordinator does not become healthy

`scripts/demo-smoke.sh` retries three temporary loopback ports. On failure it
prints the last Coordinator log lines and exits non-zero. Check local firewall
or endpoint-security rules if all attempts fail.

## Individual checks

```bash
cd agent
go test ./... -count=1
go vet ./...

cd ..
corepack pnpm --filter @acbh/coordinator build
corepack pnpm --filter @acbh/coordinator test
```
