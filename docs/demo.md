# ACBH Demo

This guide covers how to run ACBH locally for development and demonstration purposes. No real Minecraft server, public network, or cloud service is required.

## Prerequisites

| Tool | Min Version | Notes |
|------|-------------|-------|
| Go | 1.22+ | Agent compiler |
| Node.js | 20+ | Coordinator runtime |
| pnpm | 9+ | Package manager |
| curl | any | HTTP client for demo |

### Fedora

```bash
sudo dnf install golang nodejs curl
npm install -g pnpm
pnpm install
```

### Windows

```powershell
# Install Go, Node.js, and pnpm from their official sites
# Then:
pnpm install
```

Build the agent:

```bash
cd agent && go build -o acbh-agent . && cd ..
```

## Quick start

### 1. Start the Coordinator

```bash
# Default port 6121
pnpm dev:coordinator
```

Or in production mode:

```bash
pnpm build:coordinator && pnpm --filter @acbh/coordinator start
```

### 2. Run the full Demo smoke

The demo smoke script runs a complete closed loop without real Minecraft:

```bash
bash scripts/demo-smoke.sh
```

This does: build → Coordinator start → health → create group → register host → heartbeat → scan manifest → push → check latest → pull → verify restored file → group state check → cleanup.

## Agent configuration

Create a config file at `<user config dir>/acbh/config.yaml`:

```json
{
  "coordinatorUrl": "http://127.0.0.1:6121",
  "groupId": "grp_example",
  "memberId": "mem_example",
  "hostId": "host_example",
  "hostToken": "change-me",
  "displayName": "My Host",
  "deviceName": "my-device",
  "platform": "linux",
  "agentVersion": "0.1.0",
  "server": {
    "dir": "/home/user/minecraft-server",
    "command": "java -Xmx4G -Xms2G -jar fabric-server-launch.jar nogui",
    "logDir": "logs",
    "stopTimeout": "30s"
  }
}
```

A template is available at `agent/config.example.json`.

Save the file with restrictive permissions:

```bash
chmod 600 ~/.config/acbh/config.yaml
```

On Windows, the config directory is `%AppData%/acbh/config.yaml`.

## Server start (structured argv)

The recommended way to start a server is through the Local Control API, which uses structured argv:

```bash
# Start the Agent local control server
acbh-agent control serve
```

Then call with curl:

```bash
curl -X POST http://127.0.0.1:6122/v1/server/start \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <token>' \
  -d '{
    "serverDir": "/home/user/minecraft-server",
    "javaPath": "java",
    "jarPath": "fabric-server-launch.jar",
    "jvmArgs": ["-Xmx4G", "-Xms2G"],
    "serverArgs": ["nogui"]
  }'
```

The legacy `--command` string format is still supported for backward compatibility:

```bash
acbh-agent server start --server-dir /path/to/server --command "java -Xmx4G -jar server.jar nogui"
```

## Local Control safety defaults

- The local control API binds to `127.0.0.1:6122` by default.
- Binding to a non-loopback address requires explicit `--allow-remote-control`.
- All endpoints except `/health` require a bearer token.
- The token is generated on first run and stored at `<user config dir>/acbh/control-token` with `0600` permissions.
- The full token is never printed; only a masked version (`first4...last4`) is shown.

## Dashboard credentials

- The Dashboard at `/dashboard` does NOT store secrets in `localStorage`.
- `accessKey`, `hostToken`, `agentToken`, and `rconPassword` exist in page memory only.
- Use the `forgetSecrets` button to clear credentials.
- Secret input fields use `type="password"` and `autocomplete="off"`.

## Common issues

### Port occupied

```
Error: listen EADDRINUSE :::6121
```

Change the port: `PORT=6123 pnpm dev:coordinator` or `PORT=6123 pnpm build:coordinator && PORT=6123 pnpm --filter @acbh/coordinator start`.

### Token invalid

```
{"ok":false,"error":"invalid token"}
```

Check the token file: `cat ~/.config/acbh/control-token`. Copy the full token value.

### Local Control remote bind rejected

```
control: refusing non-loopback listen address
```

Pass `--allow-remote-control` only if you understand the risk. The default is loopback-only for safety.

### Manifest validation failed

```
manifest file class is required
```

Manifests must include a `class` field for each file entry. Use the Go scanner (`acbh-agent scan`) to generate valid manifests. Manifests with `fileClass` (without `class`) are automatically normalized.

### Windows path issues

When using `--command` on Windows, paths containing spaces must be quoted:

```powershell
acbh-agent server start --server-dir C:\minecraft\server --command '"C:\Program Files\Java\bin\java.exe" -jar server.jar nogui'
```

The structured argv approach (via Local Control API) avoids this issue entirely.

### Manifest schema fixtures

To verify manifest validation across Go and TS:

```bash
# Go side
cd agent && go test ./internal/manifest/ -run TestFixtures -v

# TS side
pnpm --filter @acbh/coordinator exec node --test --import tsx test/fixture.test.ts
```

## Running tests

```bash
bash scripts/verify-all.sh       # Linux/macOS
powershell scripts/verify-all.ps1 # Windows
```
