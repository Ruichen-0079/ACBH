# V1 Architecture

ACBH V1 is a distributed Minecraft host handoff platform with three roles:
a Public Node (Coordinator + relay), a Local Host (MC server + agent), and a
Player (local proxy + MC client).

## Roles

### Public Node

A low-cost public IPv4 server that provides:

- **Coordinator control plane** -- group management, host registration, heartbeat
  monitoring, election, takeover assignment, artifact metadata.
- **Artifact storage and sync** -- content-addressed SHA256 object blobs plus
  per-artifact manifests for world snapshots, server packs, and admin state.
- **Host/player rendezvous** -- the public node is the single well-known
  address. Hosts connect outward to register and heartbeat. Players connect
  outward to discover the current host and request tunnel sessions.
- **Relay fallback** -- when direct host<->player connectivity fails, the
  public node can mediate a relay stream so players can still reach the host.
  In v0.5 minimal-core Phase 2.6, the public relay accepts Minecraft TCP on
  the VPS public port, creates tunnel sessions, and forwards bytes through the
  current host agent's outbound WebSocket connection.

### Local Host

A machine on a residential or office network (likely behind NAT) that runs:

- **Minecraft server** -- managed by `acbh-agent` (start/stop/supervise).
- **`acbh-agent`** -- connects outward to the Public Node. Sends heartbeats,
  pushes artifacts, polls for takeover assignments, and restores/launches the
  server on takeover completion.
- **Can serve player tunnel streams** -- when the public node signals a new
  player session, the Host agent can accept and forward Minecraft TCP streams.
- **Can participate in takeover** -- any standby host can win election and
  become the current host.

The Host never exposes its internal Minecraft address (`127.0.0.1:25565`) to
players. Only the tunnel/relay infrastructure knows how to route player traffic
to the correct host.

### Player

The player side runs:

- **`acbh-player` / `acbh-connect`** -- a local proxy that opens a TCP
  listener, typically `127.0.0.1:25565`.
- **Minecraft client** -- connects to `localhost:25565` as if it were a normal
  server.
- **Player proxy** -- encapsulates the Minecraft TCP stream and tunnels it to
  the current Host via either relay mode or direct mode (future).

## Network Flow

### Relay-only MVP path (this PR)

```
Player MC Client -> localhost:25565 TCP
    -> acbh-player proxy
    -> [session negotiation with Public Node]
    -> Public Node relay (future)
    -> Host agent
    -> local Minecraft server
```

In the MVP relay path the Public Node creates tunnel sessions on behalf of
players. Actual byte forwarding is not built in this PR.

### Future P2P direct path (not in this PR)

```
Player MC Client -> localhost:25565 TCP
    -> acbh-player proxy
    -> [session negotiation with Public Node: exchange candidates]
    -> direct QUIC/WebRTC stream to Host agent
    -> local Minecraft server
```

### Fallback from direct to relay (not in this PR)

1. Player and Host exchange direct candidates via the Public Node.
2. Both sides attempt connectivity checks.
3. If direct connection succeeds within a timeout window, traffic flows
   directly.
4. If direct checks fail, the session falls back to relay mode through the
   Public Node.

## Design constraints

- Minecraft Java traffic is TCP. TCP hole punching is unreliable and should
  NOT be the primary direct P2P strategy.
- Future direct mode should use UDP-based transport such as QUIC or WebRTC
  data channels.
- The `acbh-player` proxy can wrap local TCP streams into QUIC/WebRTC streams.
- Relay fallback uses a public-node mediated stream when direct mode fails.

## What this PR delivers

- Architecture docs (this file, network-design.md, tunnel-protocol.md).
- TypeScript type definitions for tunnel sessions, player sessions, and host
  tunnel presence.
- In-memory tunnel session store with creation, retrieval, status updates, and
  expiration.
- Control-plane HTTP routes for player session creation and tunnel session
  planning.
- Tests covering tunnel session lifecycle, generation binding, and security
  constraints.

No actual TCP relay byte forwarding, QUIC, WebRTC, or P2P hole punching is
implemented here.
