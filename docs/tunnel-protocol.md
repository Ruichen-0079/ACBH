# Tunnel Protocol

This document defines the session types, API endpoints, and control-plane
behavior for ACBH V1 player-to-host tunneling.

The types defined here correspond to `apps/coordinator/src/network.ts`.

## Types

### TunnelMode

```typescript
type TunnelMode = "relay" | "direct";
```

### TunnelStatus

```typescript
type TunnelStatus = "pending" | "active" | "closed" | "failed" | "expired";
```

### TunnelSession

```typescript
interface TunnelSession {
  sessionId:   string;       // unique opaque id
  groupId:     string;
  hostId:      string;       // bound to currentHostId at creation
  playerId:    string;       // links to PlayerSession
  mode:        TunnelMode;
  status:      TunnelStatus;
  currentHostGeneration: number;  // frozen at creation; stale if takeover occurs
  createdAt:   string;       // ISO 8601
  expiresAt:   string;       // ISO 8601
  selectedRelayId?: string;  // relay endpoint id (relay mode)
}
```

### PlayerSession

```typescript
interface PlayerSession {
  playerId:    string;
  groupId:     string;
  displayName?: string;
  createdAt:   string;
  expiresAt:   string;
}
```

Player authentication is an **MVP placeholder**. Currently the player only
provides a `displayName`. A future PR should add proper auth (session token,
access key verification, or group-scoped player credential).

### HostTunnelPresence

```typescript
interface HostTunnelPresence {
  hostId:                 string;
  groupId:                string;
  currentHostGeneration:  number;
  supportsRelay:          boolean;
  supportsDirect:         boolean;
  lastSeenAt:             string;  // ISO 8601
}
```

The `HostTunnelPresence` deliberately does **not** expose the host's local
Minecraft address (`localMinecraftAddress`). Only the host agent knows how
to route to its local server.

### RelayEndpoint

```typescript
interface RelayEndpoint {
  relayId: string;
  host:    string;  // public relay address
  port:    number;
}
```

### DirectCandidate

```typescript
interface DirectCandidate {
  candidateId: string;
  transport:   string;      // e.g. "quic", "webrtc"
  addresses:   string[];    // host:port strings
  priority:    number;
}
```

This type is transport-neutral by design. It does not overfit to TCP hole
punching.

## API Endpoints (control-plane only)

All endpoints are session planning / control-plane only. They do **not**
carry Minecraft TCP data.

### POST /v1/groups/:groupId/player-sessions

Create a new player session.

**Request body:**

```json
{
  "displayName": "Steve"
}
```

**Response (200):**

```json
{
  "playerId": "plyr_abc123",
  "groupId": "grp_xyz",
  "displayName": "Steve",
  "createdAt": "2025-01-01T00:00:00.000Z",
  "expiresAt": "2025-01-01T00:05:00.000Z",
  "playerToken": "pt_abc123def456"
}
```

The `playerToken` is returned **once** on creation. It is stored as a SHA256
hash in memory. Subsequent `getPlayerSession` calls and tunnel session
responses do NOT include the raw token.

### POST /v1/groups/:groupId/tunnel-sessions

Create a tunnel session targeting the current host. Requires an existing
player session.

**Request body:**

```json
{
  "playerId": "plyr_abc123"
}
```

**Response (200):**

```json
{
  "sessionId": "tun_abc123",
  "groupId": "grp_xyz",
  "hostId": "host_abc",
  "playerId": "plyr_abc123",
  "mode": "relay",
  "status": "pending",
  "currentHostGeneration": 3,
  "createdAt": "2025-01-01T00:00:00.000Z",
  "expiresAt": "2025-01-01T00:05:00.000Z",
  "selectedRelayId": null
}
```

The mode is `"relay"` for MVP. Future PRs may negotiate `"direct"` based on
host capabilities.

**Error cases:**

- `404` -- group does not exist
- `404` -- player session does not exist
- `400` -- group has no current host (cannot create tunnel)

### GET /v1/groups/:groupId/tunnel-sessions/:sessionId

Retrieve metadata for an existing tunnel session. Never returns raw player
or host tokens.

**Response (200):**

Same shape as the tunnel session object above.

**Error cases:**

- `404` -- group does not exist
- `404` -- tunnel session does not exist

### GET /v1/groups/:groupId/relay/tunnel-sessions/:sessionId/host (WebSocket)

Host-side relay WebSocket endpoint. Upgrades to a WebSocket connection.

**Required headers:**

- `X-ACBH-Host-ID` -- host identifier
- `X-ACBH-Host-Token` -- host authentication token
- `X-ACBH-Host-Generation` -- must match current `currentHostGeneration`

**Behavior:**

- Validates group, host, and tunnel session exist.
- Rejects with close code `4404` if tunnel session does not exist.
- Rejects with close code `4403` if host is not current host.
- Rejects with close code `4409` if generation is stale.
- Rejects with close code `4409` if tunnel status is not `pending` or `active`.
- Rejects with close code `4410` if tunnel has expired.
- Rejects duplicate host connections for the same session (close code 4000).
- On connect, registers the host side for byte forwarding.
- When both host and player are connected, tunnel status becomes `active`.

### GET /v1/groups/:groupId/relay/tunnel-sessions/:sessionId/player (WebSocket)

Player-side relay WebSocket endpoint. Upgrades to a WebSocket connection.

**Required headers:**

- `X-ACBH-Player-ID` -- player session identifier
- `X-ACBH-Player-Token` -- player session token (returned once on creation)

**Behavior:**

- Validates group, player session, and tunnel session exist.
- Rejects with close code `4401` if player token is invalid.
- Rejects with close code `4403` if player does not match tunnel session.
- Rejects with close code `4409` if tunnel status is not `pending` or `active`.
- Rejects with close code `4410` if tunnel has expired.
- Rejects duplicate player connections for the same session (close code 4000).
- On connect, registers the player side for byte forwarding.
- When both host and player are connected, tunnel status becomes `active`.

### Relay byte forwarding semantics

- Binary WebSocket frames are forwarded opaque between host and player.
- The relay does NOT parse or inspect Minecraft protocol data.
- Frames preserve order: frames from host arrive at player in the same order
  they were sent, and vice versa.
- When one side disconnects (close or error), the other side is closed with
  code 4001 and a descriptive reason ("Host disconnected" / "Player
  disconnected").
- After both sides disconnect, the relay pair is cleaned up and tunnel status
  becomes `closed`.
- Relay runtime state (RelayPair, byte counters, active WebSocket references)
  is NOT persisted across Coordinator restarts.

## Security constraints

- Tunnel session responses must **never** expose host tokens, takeover tokens,
  host token hashes, takeover token hashes, raw player tokens, or the host's
  local Minecraft address.
- Player authentication uses a one-time token returned on session creation.
  The raw token is stored as a SHA256 hash in memory and never logged or
  persisted. The `getPlayerSession` and tunnel session endpoints never return
  the raw token.
- The `createPlayerSession` endpoint returns the raw player token once.
  Callers must store it securely.
- Host-side relay connections require valid `X-ACBH-Host-ID`,
  `X-ACBH-Host-Token`, and `X-ACBH-Host-Generation` headers.
- Player-side relay connections require valid `X-ACBH-Player-ID` and
  `X-ACBH-Player-Token` headers.
- The relay forwards opaque binary frames and does not log or store raw
  payloads. Production deployment should use HTTPS/WSS for transport security.
- Tunnel sessions and relay runtime state are ephemeral and NOT persisted
  across Coordinator restarts.

## Generation binding rules

1. A tunnel session is created targeting the current `currentHostId` and
   `currentHostGeneration`.
2. If no current host exists (null), tunnel creation is rejected.
3. The generation is frozen on the session at creation time. After a takeover
   increments the generation, the old session's `currentHostGeneration` is
   now stale. New sessions will pick up the new generation.
4. The store does not automatically expire sessions on generation mismatch.
   Callers that inspect session metadata can detect staleness by comparing
   `session.currentHostGeneration` with `group.currentHostGeneration`.

## Session TTL

Default tunnel session TTL is 300 seconds (5 minutes). Player sessions use
the same TTL. The `expireTunnelSessions(now)` method on the store cleans up
sessions past their `expiresAt`.

## Future work

- WebSocket or QUIC byte-forwarding relay on the Public Node.
- Direct candidate signaling (STUN/ICE exchange).
- QUIC or WebRTC data channel between player proxy and host agent.
- Automatic direct-to-relay fallback with connectivity probing.
- Player local proxy implementation.

## Host Agent relay client (PR28)

The Host Agent (`acbh-agent`) includes a relay host command for connecting
as the host side of a relay tunnel session.

### CLI usage

```
acbh-agent relay host \
  --coordinator-url http://public-node:8080 \
  --group-id grp_abc \
  --host-id host_abc \
  --host-token ht_xxx \
  --host-generation 3 \
  --session-id tun_abc \
  --target-address 127.0.0.1:25565
```

Flags default to values from the Agent config file when available.

### Behavior

- Connects to the Coordinator relay WebSocket endpoint
  `/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/host` with host
  auth headers.
- Dials the TCP target address (typically the local Minecraft server).
- Forwards WebSocket binary frames to TCP.
- Forwards TCP bytes to WebSocket as binary frames.
- Uses a 32 KiB buffer by default (configurable).
- Supports context cancellation for graceful shutdown.
- Closes both sides if either side fails or closes.

### Security

- The host's local Minecraft address (`--target-address`) is never exposed
  to the Coordinator or to players. The target address is a local-only
  configuration on the Host Agent.
- Host token is never logged or included in error messages.
- Binary payloads are forwarded opaque and never logged.
- No Minecraft protocol parsing is performed.
