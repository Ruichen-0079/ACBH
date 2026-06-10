export type TunnelMode = "relay" | "direct";

export type TunnelStatus = "pending" | "active" | "closed" | "failed" | "expired";

export interface TunnelSession {
  sessionId: string;
  groupId: string;
  hostId: string;
  playerId: string;
  mode: TunnelMode;
  status: TunnelStatus;
  currentHostGeneration: number;
  createdAt: string;
  expiresAt: string;
  selectedRelayId?: string;
}

export interface PlayerSession {
  playerId: string;
  groupId: string;
  displayName?: string;
  createdAt: string;
  expiresAt: string;
}

export interface HostTunnelPresence {
  hostId: string;
  groupId: string;
  currentHostGeneration: number;
  supportsRelay: boolean;
  supportsDirect: boolean;
  lastSeenAt: string;
}

export interface RelayEndpoint {
  relayId: string;
  host: string;
  port: number;
}

export interface DirectCandidate {
  candidateId: string;
  transport: string;
  addresses: string[];
  priority: number;
}
