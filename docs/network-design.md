# Network Design

This document describes the network roles, connectivity model, and session
lifecycle for ACBH V1 player-to-host tunneling.

## Network roles

### Well-known Public Node

The Public Node is the single routable address all clients know:

- **Coordinator API** -- REST endpoints for group management, host heartbeat,
  election, artifact sync, takeover, and tunnel session planning.
- **Future relay listener** -- a WebSocket (or QUIC) port for byte-stream
  relay. Not built in this PR.

Hosts and players both initiate outbound connections to the Public Node.
The Public Node never initiates connections to NAT-ed hosts or players.

### Host connectivity

- Hosts connect to the Public Node via HTTPS.
- Hosts register, send periodic heartbeats, push/pull artifacts, and
  participate in takeover.
- A host that is the current host (elected and completed takeover) is the
  target of all tunnel sessions.
- The Host agent will (in future PRs) maintain a persistent outbound
  connection (WebSocket or QUIC) to the Public Node for relay and direct
  candidate signaling.

### Player connectivity

- Players run a local proxy that opens a TCP listener.
- The proxy contacts the Public Node to look up the current host and create
  a tunnel session.
- The proxy tunnels the Minecraft TCP stream through the negotiated path.

## Session model

### Player session

A player session represents a connecting player's intent to play. It carries
a display name and an expiry. Player authentication is an MVP placeholder in
this PR; see security.md for details.

### Tunnel session

A tunnel session binds a player session to the current host at a specific
`currentHostGeneration`. Key properties:

- `sessionId` -- unique opaque identifier.
- `mode` -- `"relay"` (MVP) or `"direct"` (future).
- `status` -- `"pending"`, `"active"`, `"closed"`, `"failed"`, or `"expired"`.
- `currentHostGeneration` -- the generation at session creation time. The
  session is invalidated if the host changes (takeover occurs).
- `selectedRelayId` -- the public node relay endpoint chosen for this session
  (relay mode only).

### Session lifecycle

```
Player creates player session
    -> Player creates tunnel session targeting current host
    -> Session enters "pending" status
    -> (future) Host agent notified, mediates connection
    -> Session transitions to "active"
    -> On disconnect: "closed"
    -> On error: "failed"
    -> On TTL expiry: "expired"
```

### HostTunnelPresence

The Public Node maintains a lightweight view of which hosts are available for
tunneling:

- `hostId` -- the host.
- `currentHostGeneration` -- the generation when the presence was last updated.
- `supportsRelay` -- whether this host has a relay connection open.
- `supportsDirect` -- whether this host has published direct candidates.
- `lastSeenAt` -- heartbeat freshness indicator.

## Generation binding

Tunnel sessions bind to `currentHostGeneration` at creation time. After a
takeover increments the generation:

- Existing tunnel sessions are **not** automatically closed or expired (they
  still hold the old generation).
- New tunnel sessions must target the new `currentHostId` and new
  `currentHostGeneration`.
- A tunnel store method `expireTunnelSessions(now)` can be called to clean up
  sessions past their TTL, regardless of generation.

## Direct candidates (future)

`DirectCandidate` is a transport-neutral type representing network addresses
and protocols a host can be reached on for direct P2P:

- `candidateId` -- opaque identifier.
- `transport` -- e.g. `"quic"`, `"webrtc"`.
- `addresses` -- list of `host:port` strings.
- `priority` -- ordering hint for the player side.

This is future-facing only. No connection or candidate signaling is
implemented in this PR.

## Transport constraints

- Minecraft Java Edition uses TCP for client-server communication.
- TCP hole punching is unreliable across NAT topologies and should not be the
  primary direct P2P strategy.
- Future direct mode should layer the Minecraft TCP payload over a UDP-based
  reliable transport (QUIC or WebRTC data channel).
- The player-side proxy (`acbh-player`) is responsible for wrapping local TCP
  streams into QUIC/WebRTC frames.
- The host-side agent is responsible for unwrapping and forwarding those
  frames to the local Minecraft server.
- Relay fallback is always available through the Public Node for environments
  where direct connectivity cannot be established.

## Persistence decision

Tunnel sessions and player sessions are **ephemeral runtime state**. They are
**not persisted** across Coordinator restarts. After a restart:

- Players must reconnect and create new player sessions and tunnel sessions.
- This is intentional: tunnel sessions are short-lived transport bindings,
  not durable control-plane state. Persisting them would require reconciling
  stale session state with potentially changed host topology after restart.
- The snapshot mechanism (`StoreSnapshot`) and persistence layer deliberately
  exclude tunnel/player sessions. Only durable state from PR25 (groups, hosts,
  artifacts, election history, takeover assignments) is persisted.
