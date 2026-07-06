import { WebSocket } from "ws";
import type { InMemoryCoordinatorStore } from "./store.js";

const maxPendingBytes = 256 * 1024;
const defaultPeerWaitTimeoutMs = 15_000;

export interface RelayPair {
  sessionId: string;
  groupId: string;
  startedAt: string;
  remotePlayerAddress?: string;
  host?: { ws: WebSocket; attachedAt: string };
  player?: { ws: WebSocket; attachedAt: string };
  playerAttached: boolean;
  hostAttached: boolean;
  bothAttached: boolean;
  forwardingStarted: boolean;
  bytesHostToPlayer: number;
  bytesPlayerToHost: number;
  upstreamCopyStarted: boolean;
  downstreamCopyStarted: boolean;
  upstreamClosed: boolean;
  downstreamClosed: boolean;
  closeReason?: string;
  closeError?: string;
  closeInitiator?: string;
  pendingFromHost: Buffer[];
  pendingFromPlayer: Buffer[];
  pendingFromHostBytes: number;
  pendingFromPlayerBytes: number;
  closedAt?: string;
}

export interface RelaySessionDiagnostics {
  sessionId: string;
  startedAt: string;
  closedAt?: string;
  remotePlayerAddress?: string;
  localConnected: boolean;
  playerAttached: boolean;
  hostAttached: boolean;
  bothAttached: boolean;
  forwardingStarted: boolean;
  bytesPlayerToHost: number;
  bytesHostToPlayer: number;
  upstreamCopyStarted: boolean;
  downstreamCopyStarted: boolean;
  upstreamClosed: boolean;
  downstreamClosed: boolean;
  closeReason?: string;
  closeError?: string;
  closeInitiator?: string;
}

export interface RelayHostClient {
  groupId: string;
  hostId: string;
  connectedAt: string;
  ws: WebSocket;
}

export type RelayLogger = (event: string, meta?: Record<string, unknown>) => void;

export class RelayManager {
  private readonly pairs = new Map<string, RelayPair>();
  private readonly hostClients = new Map<string, RelayHostClient>();
  private readonly recentClosed: RelaySessionDiagnostics[] = [];
  private readonly store: InMemoryCoordinatorStore;
  private readonly logger?: RelayLogger;
  private readonly peerWaitTimeoutMs: number;

  constructor(store: InMemoryCoordinatorStore, opts?: { logger?: RelayLogger; peerWaitTimeoutMs?: number }) {
    this.store = store;
    this.logger = opts?.logger;
    this.peerWaitTimeoutMs = opts?.peerWaitTimeoutMs ?? defaultPeerWaitTimeoutMs;
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

  setRemotePlayerAddress(sessionId: string, remotePlayerAddress: string): void {
    const pair = this.ensurePair(sessionId, "");
    pair.remotePlayerAddress = remotePlayerAddress;
  }

  registerHost(sessionId: string, groupId: string, ws: WebSocket): void {
    let pair = this.pairs.get(sessionId);
    if (pair?.host) {
      ws.close(4000, "Host already connected for this session");
      return;
    }

    if (!pair) {
      pair = this.createPair(sessionId, groupId);
      this.pairs.set(sessionId, pair);
    }

    const attachedAt = new Date().toISOString();
    pair.host = { ws, attachedAt };
    pair.hostAttached = true;
    this.log("host side attached", {
      sessionId,
      groupId,
      hostAttached: true,
      playerAttached: pair.playerAttached,
    });
    this.setupForwarding(pair, "host", ws);
    this.maybeStartBridge(pair);
  }

  registerPlayer(sessionId: string, groupId: string, ws: WebSocket): void {
    let pair = this.pairs.get(sessionId);
    if (pair?.player) {
      ws.close(4000, "Player already connected for this session");
      return;
    }

    if (!pair) {
      pair = this.createPair(sessionId, groupId);
      this.pairs.set(sessionId, pair);
    }

    const attachedAt = new Date().toISOString();
    pair.player = { ws, attachedAt };
    pair.playerAttached = true;
    this.log("player side attached", {
      sessionId,
      groupId,
      hostAttached: pair.hostAttached,
      playerAttached: true,
    });
    this.setupForwarding(pair, "player", ws);
    this.maybeStartBridge(pair);
  }

  getPair(sessionId: string): RelayPair | undefined {
    return this.pairs.get(sessionId);
  }

  getPairCount(): number {
    return this.pairs.size;
  }

  isBothAttached(sessionId: string): boolean {
    const pair = this.pairs.get(sessionId);
    return pair !== undefined && pair.bothAttached && pair.forwardingStarted;
  }

  getRecentSessions(limit = 8): RelaySessionDiagnostics[] {
    const active = [...this.pairs.values()].map((pair) => this.toDiagnostics(pair));
    const closed = this.recentClosed.slice(-limit);
    return [...active, ...closed].slice(-limit);
  }

  async waitForBridge(
    sessionId: string,
    timeoutMs = this.peerWaitTimeoutMs,
    onWaiting?: (waitingFor: "host" | "player") => void,
  ): Promise<{ ok: boolean; reason?: string }> {
    const deadline = Date.now() + timeoutMs;
    let lastWaiting: "host" | "player" | null = null;
    while (Date.now() < deadline) {
      const pair = this.pairs.get(sessionId);
      if (pair?.forwardingStarted && pair.bothAttached) {
        return { ok: true };
      }
      const waitingFor: "host" | "player" = pair?.playerAttached && !pair?.hostAttached
        ? "host"
        : pair?.hostAttached && !pair?.playerAttached
          ? "player"
          : !pair?.playerAttached
            ? "player"
            : "host";
      if (onWaiting && waitingFor !== lastWaiting) {
        lastWaiting = waitingFor;
        onWaiting(waitingFor);
      }
      await sleep(50);
    }
    return { ok: false, reason: "timeout_waiting_for_peer" };
  }

  private createPair(sessionId: string, groupId: string): RelayPair {
    return {
      sessionId,
      groupId,
      startedAt: new Date().toISOString(),
      bytesHostToPlayer: 0,
      bytesPlayerToHost: 0,
      playerAttached: false,
      hostAttached: false,
      bothAttached: false,
      forwardingStarted: false,
      upstreamCopyStarted: false,
      downstreamCopyStarted: false,
      upstreamClosed: false,
      downstreamClosed: false,
      pendingFromHost: [],
      pendingFromPlayer: [],
      pendingFromHostBytes: 0,
      pendingFromPlayerBytes: 0,
    };
  }

  private ensurePair(sessionId: string, groupId: string): RelayPair {
    let pair = this.pairs.get(sessionId);
    if (!pair) {
      pair = this.createPair(sessionId, groupId);
      this.pairs.set(sessionId, pair);
    }
    return pair;
  }

  private setupForwarding(pair: RelayPair, side: "host" | "player", ws: WebSocket): void {
    ws.on("message", (data: Buffer) => {
      const chunk = Buffer.from(data);
      const target = side === "host" ? pair.player?.ws : pair.host?.ws;
      if (pair.forwardingStarted && target && target.readyState === WebSocket.OPEN) {
        if (side === "host") {
          pair.bytesHostToPlayer += chunk.length;
        } else {
          pair.bytesPlayerToHost += chunk.length;
        }
        target.send(chunk);
        return;
      }
      if (!pair.forwardingStarted) {
        this.bufferMessage(pair, side, chunk);
      }
    });

    ws.on("close", (code: number, reason: Buffer) => {
      const initiator = side === "host" ? "host" : "player";
      const reasonText = reason.length > 0 ? reason.toString() : `code=${code}`;
      if (side === "host") {
        pair.upstreamClosed = true;
      } else {
        pair.downstreamClosed = true;
      }
      this.cleanup(pair.sessionId, side, initiator, reasonText);
    });

    ws.on("error", (err: Error) => {
      const initiator = side === "host" ? "host" : "player";
      if (side === "host") {
        pair.upstreamClosed = true;
      } else {
        pair.downstreamClosed = true;
      }
      this.cleanup(pair.sessionId, side, initiator, err.message, err.message);
    });
  }

  private bufferMessage(pair: RelayPair, side: "host" | "player", chunk: Buffer): void {
    if (side === "host") {
      if (pair.pendingFromHostBytes + chunk.length > maxPendingBytes) {
        this.log("bridge buffer overflow", { sessionId: pair.sessionId, side: "host" });
        return;
      }
      pair.pendingFromHost.push(chunk);
      pair.pendingFromHostBytes += chunk.length;
      return;
    }
    if (pair.pendingFromPlayerBytes + chunk.length > maxPendingBytes) {
      this.log("bridge buffer overflow", { sessionId: pair.sessionId, side: "player" });
      return;
    }
    pair.pendingFromPlayer.push(chunk);
    pair.pendingFromPlayerBytes += chunk.length;
  }

  private flushBuffers(pair: RelayPair): void {
    if (pair.player?.ws && pair.player.ws.readyState === WebSocket.OPEN) {
      for (const chunk of pair.pendingFromHost) {
        pair.player.ws.send(chunk);
        pair.bytesHostToPlayer += chunk.length;
      }
    }
    pair.pendingFromHost = [];
    pair.pendingFromHostBytes = 0;

    if (pair.host?.ws && pair.host.ws.readyState === WebSocket.OPEN) {
      for (const chunk of pair.pendingFromPlayer) {
        pair.host.ws.send(chunk);
        pair.bytesPlayerToHost += chunk.length;
      }
    }
    pair.pendingFromPlayer = [];
    pair.pendingFromPlayerBytes = 0;
  }

  private maybeStartBridge(pair: RelayPair): void {
    if (!pair.host?.ws || !pair.player?.ws) {
      if (pair.playerAttached && !pair.hostAttached) {
        this.log("waiting for host side", { sessionId: pair.sessionId, groupId: pair.groupId });
      } else if (pair.hostAttached && !pair.playerAttached) {
        this.log("waiting for player side", { sessionId: pair.sessionId, groupId: pair.groupId });
      }
      return;
    }
    if (pair.host.ws.readyState !== WebSocket.OPEN || pair.player.ws.readyState !== WebSocket.OPEN) {
      return;
    }
    if (pair.forwardingStarted) {
      return;
    }

    pair.bothAttached = true;
    pair.forwardingStarted = true;
    pair.upstreamCopyStarted = true;
    pair.downstreamCopyStarted = true;
    this.flushBuffers(pair);

    this.log("both sides attached", {
      sessionId: pair.sessionId,
      groupId: pair.groupId,
      playerAttached: true,
      hostAttached: true,
    });
    this.log("bidirectional bridge started", {
      sessionId: pair.sessionId,
      groupId: pair.groupId,
      bytesPlayerToHost: pair.bytesPlayerToHost,
      bytesHostToPlayer: pair.bytesHostToPlayer,
      copyStartedUpstream: pair.upstreamCopyStarted,
      copyStartedDownstream: pair.downstreamCopyStarted,
    });

    try {
      this.store.updateTunnelSessionStatus(pair.groupId, pair.sessionId, "active");
    } catch {
      // store error on activation is non-fatal; relay still forwards
    }
  }

  private cleanup(
    sessionId: string,
    side: "host" | "player",
    closeInitiator: string,
    closeReason: string,
    closeError?: string,
  ): void {
    const pair = this.pairs.get(sessionId);
    if (!pair) return;

    pair.closeInitiator = closeInitiator;
    pair.closeReason = closeReason;
    pair.closeError = closeError;

    if (side === "host") {
      pair.host = undefined;
      pair.hostAttached = false;
      if (pair.player?.ws && pair.player.ws.readyState === WebSocket.OPEN) {
        pair.player.ws.close(4001, "Host disconnected");
      }
    } else {
      pair.player = undefined;
      pair.playerAttached = false;
      if (pair.host?.ws && pair.host.ws.readyState === WebSocket.OPEN) {
        pair.host.ws.close(4001, "Player disconnected");
      }
    }

    const fullyClosed = !pair.host && !pair.player;
    if (fullyClosed) {
      pair.closedAt = new Date().toISOString();
      pair.bothAttached = false;
      pair.forwardingStarted = false;
      this.log("bridge closed", {
        sessionId,
        groupId: pair.groupId,
        playerAttached: false,
        hostAttached: false,
        bytesPlayerToHost: pair.bytesPlayerToHost,
        bytesHostToPlayer: pair.bytesHostToPlayer,
        upstreamClosed: pair.upstreamClosed,
        downstreamClosed: pair.downstreamClosed,
        closeInitiator: pair.closeInitiator,
        closeReason: pair.closeReason,
        closeError: pair.closeError,
      });
      this.rememberClosed(pair);
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

  private rememberClosed(pair: RelayPair): void {
    this.recentClosed.push(this.toDiagnostics(pair));
    if (this.recentClosed.length > 32) {
      this.recentClosed.splice(0, this.recentClosed.length - 32);
    }
  }

  private toDiagnostics(pair: RelayPair): RelaySessionDiagnostics {
    return {
      sessionId: pair.sessionId,
      startedAt: pair.startedAt,
      closedAt: pair.closedAt,
      remotePlayerAddress: pair.remotePlayerAddress,
      localConnected: pair.hostAttached,
      playerAttached: pair.playerAttached,
      hostAttached: pair.hostAttached,
      bothAttached: pair.bothAttached,
      forwardingStarted: pair.forwardingStarted,
      bytesPlayerToHost: pair.bytesPlayerToHost,
      bytesHostToPlayer: pair.bytesHostToPlayer,
      upstreamCopyStarted: pair.upstreamCopyStarted,
      downstreamCopyStarted: pair.downstreamCopyStarted,
      upstreamClosed: pair.upstreamClosed,
      downstreamClosed: pair.downstreamClosed,
      closeReason: pair.closeReason,
      closeError: pair.closeError,
      closeInitiator: pair.closeInitiator,
    };
  }

  private log(event: string, meta: Record<string, unknown> = {}): void {
    this.logger?.(event, { event, ...meta });
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}