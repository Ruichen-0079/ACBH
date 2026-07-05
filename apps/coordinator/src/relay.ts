import { WebSocket, type RawData } from "ws";
import type { InMemoryCoordinatorStore } from "./store.js";

const maxQueuedBytesPerDirection = 1024 * 1024;

export interface RelayPair {
  sessionId: string;
  groupId: string;
  host?: { ws: WebSocket };
  player?: { ws: WebSocket };
  bytesHostToPlayer: number;
  bytesPlayerToHost: number;
  queuedHostToPlayer: Buffer[];
  queuedPlayerToHost: Buffer[];
  queuedHostToPlayerBytes: number;
  queuedPlayerToHostBytes: number;
  closeReason?: string;
  lastError?: string;
  closedAt?: string;
}

export class RelayManager {
  private readonly pairs = new Map<string, RelayPair>();
  private readonly store: InMemoryCoordinatorStore;

  constructor(store: InMemoryCoordinatorStore) {
    this.store = store;
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
        queuedHostToPlayer: [],
        queuedPlayerToHost: [],
        queuedHostToPlayerBytes: 0,
        queuedPlayerToHostBytes: 0,
      };
      this.pairs.set(sessionId, pair);
    }

    pair.host = { ws };
    this.setupForwarding(pair, "host", ws);
    this.maybeActivate(pair);
    this.flushQueued(pair, "host");
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
        queuedHostToPlayer: [],
        queuedPlayerToHost: [],
        queuedHostToPlayerBytes: 0,
        queuedPlayerToHostBytes: 0,
      };
      this.pairs.set(sessionId, pair);
    }

    pair.player = { ws };
    this.setupForwarding(pair, "player", ws);
    this.maybeActivate(pair);
    this.flushQueued(pair, "player");
  }

  getPair(sessionId: string): RelayPair | undefined {
    return this.pairs.get(sessionId);
  }

  getPairCount(): number {
    return this.pairs.size;
  }

  private setupForwarding(pair: RelayPair, side: "host" | "player", ws: WebSocket): void {
    ws.on("message", (data) => {
      const payload = rawDataToBuffer(data);
      const target = side === "host" ? pair.player?.ws : pair.host?.ws;
      if (target && target.readyState === WebSocket.OPEN) {
        if (side === "host") {
          pair.bytesHostToPlayer += payload.length;
        } else {
          pair.bytesPlayerToHost += payload.length;
        }
        target.send(payload, { binary: true }, (err) => {
          if (err) {
            pair.lastError = String(err);
          }
        });
        return;
      }

      if (side === "host") {
        this.enqueue(pair, "hostToPlayer", payload, ws);
      } else {
        this.enqueue(pair, "playerToHost", payload, ws);
      }
    });

    ws.on("close", (code: number, reason: Buffer) => {
      this.cleanup(pair.sessionId, side, `${code} ${reason.toString()}`.trim());
    });

    ws.on("error", (err) => {
      pair.lastError = String(err);
      this.cleanup(pair.sessionId, side, String(err));
    });
  }

  private enqueue(pair: RelayPair, direction: "hostToPlayer" | "playerToHost", payload: Buffer, source: WebSocket): void {
    if (direction === "hostToPlayer") {
      if (pair.queuedHostToPlayerBytes + payload.length > maxQueuedBytesPerDirection) {
        pair.lastError = "relay queue overflow while waiting for player websocket";
        source.close(1013, pair.lastError);
        return;
      }
      pair.queuedHostToPlayer.push(Buffer.from(payload));
      pair.queuedHostToPlayerBytes += payload.length;
      return;
    }

    if (pair.queuedPlayerToHostBytes + payload.length > maxQueuedBytesPerDirection) {
      pair.lastError = "relay queue overflow while waiting for host websocket";
      source.close(1013, pair.lastError);
      return;
    }
    pair.queuedPlayerToHost.push(Buffer.from(payload));
    pair.queuedPlayerToHostBytes += payload.length;
  }

  private flushQueued(pair: RelayPair, connectedSide: "host" | "player"): void {
    if (connectedSide === "host" && pair.host?.ws.readyState === WebSocket.OPEN) {
      const target = pair.host.ws;
      for (const payload of pair.queuedPlayerToHost) {
        pair.bytesPlayerToHost += payload.length;
        target.send(payload, { binary: true }, (err) => {
          if (err) {
            pair.lastError = String(err);
          }
        });
      }
      pair.queuedPlayerToHost = [];
      pair.queuedPlayerToHostBytes = 0;
    }

    if (connectedSide === "player" && pair.player?.ws.readyState === WebSocket.OPEN) {
      const target = pair.player.ws;
      for (const payload of pair.queuedHostToPlayer) {
        pair.bytesHostToPlayer += payload.length;
        target.send(payload, { binary: true }, (err) => {
          if (err) {
            pair.lastError = String(err);
          }
        });
      }
      pair.queuedHostToPlayer = [];
      pair.queuedHostToPlayerBytes = 0;
    }
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

  private cleanup(sessionId: string, side: "host" | "player", reason: string): void {
    const pair = this.pairs.get(sessionId);
    if (!pair) return;
    pair.closeReason = reason;

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

function rawDataToBuffer(data: RawData): Buffer {
  if (Buffer.isBuffer(data)) {
    return Buffer.from(data);
  }
  if (Array.isArray(data)) {
    return Buffer.concat(data.map((part) => Buffer.from(part)));
  }
  return Buffer.from(data);
}
