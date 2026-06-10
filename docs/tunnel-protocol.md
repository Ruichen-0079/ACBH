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
  "expiresAt": "2025-01-01T00:05:00.000Z"
}
```

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
- `400` -- current host generation is zero (no takeover has completed)

### GET /v1/groups/:groupId/tunnel-sessions/:sessionId

Retrieve metadata for an existing tunnel session.

**Response (200):**

Same shape as the tunnel session object above.

**Error cases:**

- `404` -- group does not exist
- `404` -- tunnel session does not exist

## Security constraints

- Tunnel session responses must **never** expose host tokens, takeover tokens,
  or the host's local Minecraft address.
- Player authentication is an MVP placeholder. The `POST /v1/groups/:groupId/player-sessions`
  endpoint currently accepts only a `displayName`. A future PR should add
  proper player auth (e.g. group access key verification or session tokens).
- Only the public endpoints listed above are exposed. No raw host credentials
  are returned through tunnel endpoints.
- Tunnel sessions are ephemeral runtime state and are NOT persisted across
  Coordinator restarts.

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
- Proper player authentication (tokens or access key verification).
