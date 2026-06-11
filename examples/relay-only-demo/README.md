# ACBH Relay-Only Demo

This directory contains a self-contained demo of the ACBH relay-only path:

```
TCP demo client
-> Player local TCP proxy
-> Public Relay (in-memory)
-> Host Agent relay client
-> Host local TCP echo server
-> (echo back)
```

No real Minecraft server, Coordinator, or network setup is required.
Everything runs in-process with random local ports.

## Quick start

```bash
./run.sh
```

Or from the repo root:

```bash
cd agent && go run ./cmd/relay-demo
```

## What it does

1. Starts an in-memory relay pair server (simulates the Public Node relay).
2. Starts a local TCP echo server (simulates a Minecraft/Velocity server).
3. Starts the Host Agent relay client pointing to the echo server.
4. Starts the Player local proxy listening on a random TCP port.
5. Connects a local TCP test client to the proxy.
6. Sends a 4-byte payload and verifies the exact echo.
7. Sends multiple frames and verifies they are echoed in order.
8. Shuts down cleanly.

## Options

| Flag | Default | Description |
|---|---|---|
| `--host-target` | auto | Host relay target address (e.g. `127.0.0.1:25577`) |
| `--player-listen` | auto | Player listen address (e.g. `127.0.0.1:25565`) |
| `--timeout` | 10s | Demo timeout |

Examples:

```bash
# Default (auto ports)
go run ./cmd/relay-demo

# Custom addresses
go run ./cmd/relay-demo \
  --host-target 127.0.0.1:25577 \
  --player-listen 127.0.0.1:25565

# Short timeout for CI
go run ./cmd/relay-demo --timeout 5s
```

## Important

- **Independent addresses**: The Host target address and Player listen address
  are independent. If your Host uses Velocity on `127.0.0.1:25577`, set
  `--host-target 127.0.0.1:25577`. The Player can listen on `127.0.0.1:25565`.
- **Random ports by default**: When `--host-target` and `--player-listen` are
  not set, random available ports are used to avoid conflicts.
- **No real servers**: The echo server is a simple in-memory TCP echo — no
  Minecraft jar, Velocity, or Java is required.
- **Cleanup**: The demo handles SIGINT/SIGTERM for clean shutdown.
