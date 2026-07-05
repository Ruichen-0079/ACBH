import { WebSocket } from "ws";
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
  private staleCheckTimer: ReturnType<typeof setInterval> | null = null;

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
      this.sockets.add(socket);
      this.activeConnections += 1;
      socket.once("close", () => {
        this.sockets.delete(socket);
        this.activeConnections = Math.max(0, this.activeConnections - 1);
      });
      this.handleIncoming(socket).catch((err) => {
        this.lastError = String(err);
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

  status(): PublicRelayStatus {
    return {
      configured: this.opts.port > 0,
      publicListenerActive: this.server !== null && this.server.listening,
      publicEndpoint: this.opts.port > 0 ? `${this.opts.host}:${this.opts.port}` : null,
      activeConnections: this.activeConnections,
      lastError: this.lastError,
    };
  }

  private async handleIncoming(tcpConn: net.Socket): Promise<void> {
    const groups = this.opts.store.listGroups();
    if (groups.length === 0) {
      tcpConn.end("No groups configured\n");
      return;
    }

    const group = selectPublicRelayGroup(groups, new Date(), this.opts.store.heartbeatTimeoutMs);
    if (group === null) {
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
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(chunk);
      }
    });
    tcpConn.on("close", closeAll);
    tcpConn.on("error", closeAll);

    // ws -> tcp
    ws.on("message", (data: Buffer) => {
      if (!tcpConn.destroyed) {
        tcpConn.write(data);
      }
    });
    ws.on("close", closeAll);
    ws.on("error", closeAll);

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
}
