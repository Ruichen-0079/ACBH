import { WebSocket } from "ws";
import type { InMemoryCoordinatorStore } from "./store.js";

export interface RelayPair {
  sessionId: string;
  groupId: string;
  host?: { ws: WebSocket };
  player?: { ws: WebSocket };
  bytesHostToPlayer: number;
  bytesPlayerToHost: number;
  closedAt?: string;
}

export interface RelayHostClient {
  groupId: string;
  hostId: string;
  connectedAt: string;
  ws: WebSocket;
}

export class RelayManager {
  private readonly pairs = new Map<string, RelayPair>();
  private readonly hostClients = new Map<string, RelayHostClient>();
  private readonly store: InMemoryCoordinatorStore;

  constructor(store: InMemoryCoordinatorStore) {
    this.store = store;
  }

  registerHostClient(groupId: string, hostId: string, ws: WebSocket): void {
    const key = `${groupId}:${hostId}`;
    const existing = this.hostClients.get(key);
    if (existing && existing.ws.readyState === WebSocket.OPEN) {
      try { existing.ws.close(4000, "Replaced by newer relay client"); } catch {}
    }
    const client: RelayHostClient = {
      groupId,
      hostId,
      connectedAt: new Date().toISOString(),
      ws,
    };
    this.hostClients.set(key, client);
    ws.on("close", () => {
      const current = this.hostClients.get(key);
      if (current?.ws === ws) {
        this.hostClients.delete(key);
      }
    });
    ws.on("error", () => {
      const current = this.hostClients.get(key);
      if (current?.ws === ws) {
        this.hostClients.delete(key);
      }
    });
  }

  hasHostClient(groupId: string, hostId: string): boolean {
    const client = this.hostClients.get(`${groupId}:${hostId}`);
    return client !== undefined && client.ws.readyState === WebSocket.OPEN;
  }

  getHostClient(groupId: string, hostId: string): RelayHostClient | undefined {
    const client = this.hostClients.get(`${groupId}:${hostId}`);
    if (client && client.ws.readyState === WebSocket.OPEN) {
      return client;
    }
    return undefined;
  }

  registerHost(sessionId: string, groupId: string, ws: WebSocket): void {
    let pair = this.pairs.get(sessionId);
    if (pair?.host) {
      ws.close(4000, "Host already connected for this session");
      return;
    }

    if (!pair) {
      pair = {
        sessionId,
        groupId,
        bytesHostToPlayer: 0,
        bytesPlayerToHost: 0,
      };
      this.pairs.set(sessionId, pair);
    }

    pair.host = { ws };
    this.setupForwarding(pair, "host", ws);
    this.maybeActivate(pair);
  }

  registerPlayer(sessionId: string, groupId: string, ws: WebSocket): void {
    let pair = this.pairs.get(sessionId);
    if (pair?.player) {
      ws.close(4000, "Player already connected for this session");
      return;
    }

    if (!pair) {
      pair = {
        sessionId,
        groupId,
        bytesHostToPlayer: 0,
        bytesPlayerToHost: 0,
      };
      this.pairs.set(sessionId, pair);
    }

    pair.player = { ws };
    this.setupForwarding(pair, "player", ws);
    this.maybeActivate(pair);
  }

  getPair(sessionId: string): RelayPair | undefined {
    return this.pairs.get(sessionId);
  }

  getPairCount(): number {
    return this.pairs.size;
  }

  private setupForwarding(pair: RelayPair, side: "host" | "player", ws: WebSocket): void {
    ws.on("message", (data: Buffer) => {
      const target = side === "host" ? pair.player?.ws : pair.host?.ws;
      if (target && target.readyState === WebSocket.OPEN) {
        if (side === "host") {
          pair.bytesHostToPlayer += data.length;
        } else {
          pair.bytesPlayerToHost += data.length;
        }
        target.send(data);
      }
    });

    ws.on("close", (_code: number) => {
      this.cleanup(pair.sessionId, side);
    });

    ws.on("error", () => {
      this.cleanup(pair.sessionId, side);
    });
  }

  private maybeActivate(pair: RelayPair): void {
    if (pair.host && pair.player) {
      try {
        this.store.updateTunnelSessionStatus(pair.groupId, pair.sessionId, "active");
      } catch {
        // store error on activation is non-fatal; relay still forwards
      }
    }
  }

  private cleanup(sessionId: string, side: "host" | "player"): void {
    const pair = this.pairs.get(sessionId);
    if (!pair) return;

    if (side === "host") {
      pair.host = undefined;
      if (pair.player?.ws && pair.player.ws.readyState === WebSocket.OPEN) {
        pair.player.ws.close(4001, "Host disconnected");
      }
    } else {
      pair.player = undefined;
      if (pair.host?.ws && pair.host.ws.readyState === WebSocket.OPEN) {
        pair.host.ws.close(4001, "Player disconnected");
      }
    }

    if (!pair.host && !pair.player) {
      pair.closedAt = new Date().toISOString();
      try {
        this.store.updateTunnelSessionStatus(pair.groupId, sessionId, "closed");
      } catch {
        // non-fatal if update fails
      }
      this.pairs.delete(sessionId);
    } else {
      try {
        this.store.updateTunnelSessionStatus(pair.groupId, sessionId, "closed");
      } catch {
        // non-fatal
      }
    }
  }
}
