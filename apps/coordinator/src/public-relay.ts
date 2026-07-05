import { WebSocket, type RawData } from "ws";
import * as net from "net";
import type { InMemoryCoordinatorStore, PublicGroupState } from "./store.js";
import type { RelayManager } from "./relay.js";

export interface PublicRelayOptions {
  host: string;
  port: number;
  coordinatorBaseURL: string; // e.g. "http://127.0.0.1:6121" for constructing WS URLs internally
  store: InMemoryCoordinatorStore;
  relay: RelayManager;
  logger?: (msg: string, meta?: any) => void;
}

export interface PublicRelayStatus {
  configured: boolean;
  publicListenerActive: boolean;
  publicEndpoint: string | null;
  activeConnections: number;
  lastError: string | null;
  recentConnections: PublicRelayConnectionDebug[];
}

export interface PublicRelayConnectionDebug {
  connectionId: string;
  sessionId: string | null;
  playerRemoteAddr: string | null;
  hostConnected: boolean;
  localDialAttempted: boolean;
  localDialSucceeded: boolean;
  localEndpoint: string | null;
  bytesPlayerToCoordinator: number;
  bytesCoordinatorToHost: number;
  bytesHostToLocal: number;
  bytesLocalToHost: number;
  bytesHostToCoordinator: number;
  bytesCoordinatorToPlayer: number;
  closeReason: string | null;
  lastError: string | null;
  openedAt: string;
  closedAt: string | null;
}

export function selectPublicRelayGroup(
  groups: PublicGroupState[],
  now = new Date(),
  heartbeatTimeoutMs = 30_000,
): PublicGroupState | null {
  const candidates = groups
    .map((group) => {
      const currentHost = group.currentHostId === null
        ? undefined
        : group.hosts.find((host) => host.hostId === group.currentHostId);
      const heartbeatMs = currentHost?.lastHeartbeatAt === null || currentHost?.lastHeartbeatAt === undefined
        ? Number.NaN
        : Date.parse(currentHost.lastHeartbeatAt);
      const fresh = Number.isFinite(heartbeatMs) && now.getTime() - heartbeatMs <= heartbeatTimeoutMs;
      const statusRank = currentHost?.status === "hosting" ? 3 : currentHost?.status === "online" ? 2 : currentHost?.status === "standby" ? 1 : 0;
      return { group, currentHost, heartbeatMs, fresh, statusRank };
    })
    .filter((candidate) => candidate.currentHost !== undefined && candidate.fresh);

  if (candidates.length === 0) {
    return null;
  }

  candidates.sort((a, b) => (
    b.statusRank - a.statusRank ||
    b.group.currentHostGeneration - a.group.currentHostGeneration ||
    b.heartbeatMs - a.heartbeatMs ||
    a.group.groupId.localeCompare(b.group.groupId)
  ));
  return candidates[0].group;
}

export class PublicRelayIngress {
  private server: net.Server | null = null;
  private opts: PublicRelayOptions;
  private activeConnections = 0;
  private lastError: string | null = null;
  private readonly sockets = new Set<net.Socket>();
  private readonly playerSockets = new Set<WebSocket>();
  private readonly connections = new Map<string, PublicRelayConnectionDebug>();
  private staleCheckTimer: ReturnType<typeof setInterval> | null = null;
  private nextConnectionID = 1;

  constructor(opts: PublicRelayOptions) {
    this.opts = opts;
  }

  async start(port?: number): Promise<void> {
    const nextPort = port !== undefined && Number.isFinite(port) && port > 0 ? port : this.opts.port;
    if (this.server) {
      if (nextPort === this.opts.port) {
        return;
      }
      this.stop();
    }
    this.opts.port = nextPort;
    if (this.opts.port <= 0) {
      this.opts.logger?.("Public relay ingress disabled (port <= 0)");
      return;
    }
    this.lastError = null;

    const server = net.createServer((socket) => {
      const debug = this.newConnectionDebug(socket);
      this.sockets.add(socket);
      this.activeConnections += 1;
      socket.once("close", () => {
        this.sockets.delete(socket);
        this.activeConnections = Math.max(0, this.activeConnections - 1);
        this.closeConnectionDebug(debug, "player tcp closed");
      });
      this.handleIncoming(socket).catch((err) => {
        this.lastError = String(err);
        debug.lastError = String(err);
        this.closeConnectionDebug(debug, String(err));
        this.opts.logger?.("Public relay ingress connection handler error", { err: String(err) });
        try { socket.destroy(); } catch {}
      });
    });
    this.server = server;

    await new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        server.off("error", onError);
        server.off("listening", onListening);
      };
      const onError = (err: Error) => {
        cleanup();
        this.lastError = String(err);
        if (this.server === server) {
          this.server = null;
        }
        reject(err);
      };
      const onListening = () => {
        cleanup();
        this.opts.logger?.(`ACBH public relay ingress listening on ${this.opts.host}:${this.opts.port} (players connect here)`);
        this.startStaleCheck();
        resolve();
      };
      server.once("error", onError);
      server.once("listening", onListening);
      server.listen(this.opts.port, this.opts.host);
    });

    server.on("error", (err) => {
      this.lastError = String(err);
      this.opts.logger?.("Public relay ingress server error", { err: String(err) });
    });
  }

  stop(): void {
    if (this.staleCheckTimer) {
      clearInterval(this.staleCheckTimer);
      this.staleCheckTimer = null;
    }
    if (this.server) {
      this.server.close();
      this.server = null;
    }
    for (const socket of this.sockets) {
      try { socket.destroy(); } catch {}
    }
    this.sockets.clear();
    for (const ws of this.playerSockets) {
      try {
        if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
          ws.close(1001, "public relay stopped");
        } else {
          ws.terminate();
        }
      } catch {}
    }
    this.playerSockets.clear();
    this.activeConnections = 0;
  }

  status(endpointHost?: string): PublicRelayStatus {
    return {
      configured: this.opts.port > 0,
      publicListenerActive: this.server !== null && this.server.listening,
      publicEndpoint: this.opts.port > 0 ? `${this.publicEndpointHost(endpointHost)}:${this.opts.port}` : null,
      activeConnections: this.activeConnections,
      lastError: this.lastError,
      recentConnections: this.recentConnections(),
    };
  }

  private async handleIncoming(tcpConn: net.Socket): Promise<void> {
    const debug = this.connectionDebugFor(tcpConn);
    const groups = this.opts.store.listGroups();
    if (groups.length === 0) {
      if (debug) {
        debug.closeReason = "no groups configured";
      }
      tcpConn.end("No groups configured\n");
      return;
    }

    const group = selectPublicRelayGroup(groups, new Date(), this.opts.store.heartbeatTimeoutMs);
    if (group === null) {
      if (debug) {
        debug.closeReason = "no current host available for relay";
      }
      tcpConn.end("No current host available for relay\n");
      return;
    }
    const groupId = group.groupId;

    // Create player session (unauthenticated creation is supported)
    let playerSess: any;
    try {
      playerSess = this.opts.store.createPlayerSession({ groupId, displayName: "public-mc-player" });
    } catch (e) {
      this.opts.logger?.("Failed to create player session", { err: String(e) });
      tcpConn.destroy();
      return;
    }

    // Create tunnel session (will be for current host)
    let tun: any;
    try {
      tun = this.opts.store.createTunnelSession({ groupId, playerId: playerSess.playerId });
    } catch (e) {
      this.opts.logger?.("Failed to create tunnel session for public relay", { err: String(e) });
      tcpConn.destroy();
      return;
    }

    const sessionId = tun.sessionId;
    if (debug) {
      debug.sessionId = sessionId;
    }

    // Connect as the player side (this will trigger the WS route handler and registerPlayer)
    const wsURL = this.buildWSURL(groupId, sessionId);
    const headers: Record<string, string> = {};
    headers["X-ACBH-Player-ID"] = playerSess.playerId;
    headers["X-ACBH-Player-Token"] = playerSess.playerToken;

    let ws: WebSocket;
    try {
      ws = new WebSocket(wsURL, { headers });
      await new Promise<void>((resolve, reject) => {
        const t = setTimeout(() => reject(new Error("player WS open timeout")), 10000);
        ws.once("open", () => { clearTimeout(t); resolve(); });
        ws.once("error", (e) => { clearTimeout(t); reject(e); });
        ws.once("close", () => { clearTimeout(t); reject(new Error("closed before open")); });
      });
    } catch (e) {
      if (debug) {
        debug.lastError = String(e);
        this.closeConnectionDebug(debug, "player websocket connect failed");
      }
      this.opts.logger?.("Public relay player WS connect failed", { err: String(e), sessionId });
      tcpConn.destroy();
      return;
    }

    // Bidirectional forward (same as playerproxy logic)
    this.playerSockets.add(ws);
    let closed = false;
    const closeAll = () => {
      if (closed) return;
      closed = true;
      this.playerSockets.delete(ws);
      try { tcpConn.destroy(); } catch {}
      try { if (ws.readyState === WebSocket.OPEN) ws.close(1000, "public relay close"); } catch {}
    };

    // tcp -> ws
    tcpConn.on("data", (chunk: Buffer) => {
      if (debug) {
        debug.bytesPlayerToCoordinator += chunk.length;
      }
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(chunk, { binary: true }, (err) => {
          if (err && debug) {
            debug.lastError = String(err);
          }
        });
      }
    });
    tcpConn.on("close", () => {
      this.closeConnectionDebug(debug, "player tcp closed");
      closeAll();
    });
    tcpConn.on("error", (err) => {
      if (debug) {
        debug.lastError = String(err);
      }
      this.closeConnectionDebug(debug, String(err));
      closeAll();
    });

    // ws -> tcp
    ws.on("message", (data) => {
      const payload = rawDataToBuffer(data);
      if (debug) {
        debug.bytesCoordinatorToPlayer += payload.length;
      }
      if (!tcpConn.destroyed) {
        tcpConn.write(payload);
      }
    });
    ws.on("close", (code, reason) => {
      this.closeConnectionDebug(debug, `player websocket closed ${code} ${reason.toString()}`.trim());
      closeAll();
    });
    ws.on("error", (err) => {
      if (debug) {
        debug.lastError = String(err);
      }
      this.closeConnectionDebug(debug, String(err));
      closeAll();
    });

    // context cancel not needed for now
  }

  private buildWSURL(groupId: string, sessionId: string): string {
    // Convert http(s) to ws(s) and point to localhost for internal connect
    let base = this.opts.coordinatorBaseURL || "http://127.0.0.1:6121";
    base = base.replace(/^https:/, "wss:").replace(/^http:/, "ws:");
    // ensure no trailing slash issues
    if (base.endsWith("/")) base = base.slice(0, -1);
    return `${base}/v1/groups/${groupId}/relay/tunnel-sessions/${sessionId}/player`;
  }

  private startStaleCheck(): void {
    if (this.staleCheckTimer) {
      clearInterval(this.staleCheckTimer);
    }
    this.staleCheckTimer = setInterval(() => {
      const group = selectPublicRelayGroup(this.opts.store.listGroups(), new Date(), this.opts.store.heartbeatTimeoutMs);
      if (group !== null) {
        return;
      }
      this.lastError = "public relay stopped because no current host heartbeat is fresh";
      this.opts.logger?.("Public relay ingress stopped because no current host heartbeat is fresh");
      this.stop();
    }, Math.min(5_000, Math.max(100, this.opts.store.heartbeatTimeoutMs)));
    this.staleCheckTimer.unref?.();
  }

  private newConnectionDebug(socket: net.Socket): PublicRelayConnectionDebug {
    const debug: PublicRelayConnectionDebug = {
      connectionId: `pub_${Date.now().toString(36)}_${this.nextConnectionID++}`,
      sessionId: null,
      playerRemoteAddr: socket.remoteAddress && socket.remotePort ? `${socket.remoteAddress}:${socket.remotePort}` : null,
      hostConnected: false,
      localDialAttempted: false,
      localDialSucceeded: false,
      localEndpoint: null,
      bytesPlayerToCoordinator: 0,
      bytesCoordinatorToHost: 0,
      bytesHostToLocal: 0,
      bytesLocalToHost: 0,
      bytesHostToCoordinator: 0,
      bytesCoordinatorToPlayer: 0,
      closeReason: null,
      lastError: null,
      openedAt: new Date().toISOString(),
      closedAt: null,
    };
    this.connections.set(debug.connectionId, debug);
    (socket as net.Socket & { acbhConnectionID?: string }).acbhConnectionID = debug.connectionId;
    this.trimConnections();
    return debug;
  }

  private connectionDebugFor(socket: net.Socket): PublicRelayConnectionDebug | undefined {
    const id = (socket as net.Socket & { acbhConnectionID?: string }).acbhConnectionID;
    return id ? this.connections.get(id) : undefined;
  }

  private closeConnectionDebug(debug: PublicRelayConnectionDebug | undefined, reason: string): void {
    if (!debug) {
      return;
    }
    const pair = debug.sessionId ? this.opts.relay.getPair(debug.sessionId) : undefined;
    if (pair) {
      debug.hostConnected = pair.host?.ws.readyState === WebSocket.OPEN;
      debug.bytesCoordinatorToHost = pair.bytesPlayerToHost;
      debug.bytesHostToCoordinator = pair.bytesHostToPlayer;
    }
    if (debug.closedAt === null) {
      debug.closedAt = new Date().toISOString();
    }
    debug.closeReason = debug.closeReason ?? reason;
  }

  private recentConnections(): PublicRelayConnectionDebug[] {
    const out = Array.from(this.connections.values()).slice(-20).map((debug) => {
      const pair = debug.sessionId ? this.opts.relay.getPair(debug.sessionId) : undefined;
      if (pair) {
        return {
          ...debug,
          hostConnected: pair.host?.ws.readyState === WebSocket.OPEN,
          bytesCoordinatorToHost: pair.bytesPlayerToHost,
          bytesHostToCoordinator: pair.bytesHostToPlayer,
          lastError: debug.lastError ?? pair.lastError ?? null,
          closeReason: debug.closeReason ?? pair.closeReason ?? null,
        };
      }
      return { ...debug };
    });
    return out.reverse();
  }

  private trimConnections(): void {
    const extra = this.connections.size - 20;
    if (extra <= 0) {
      return;
    }
    for (const key of Array.from(this.connections.keys()).slice(0, extra)) {
      this.connections.delete(key);
    }
  }

  private publicEndpointHost(endpointHost?: string): string {
    if (this.opts.host !== "0.0.0.0" && this.opts.host !== "::" && this.opts.host !== "") {
      return this.opts.host;
    }
    const cleaned = endpointHost?.split(":")[0]?.trim();
    if (cleaned && cleaned !== "0.0.0.0" && cleaned !== "::") {
      return cleaned;
    }
    return this.opts.host;
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
