import { WebSocket } from "ws";
import * as net from "net";
import type { InMemoryCoordinatorStore } from "./store.js";
import type { RelayManager } from "./relay.js";

export interface PublicRelayOptions {
  host: string;
  port: number;
  coordinatorBaseURL: string; // e.g. "http://127.0.0.1:6121" for constructing WS URLs internally
  store: InMemoryCoordinatorStore;
  relay: RelayManager;
  logger?: (msg: string, meta?: any) => void;
}

const defaultBufferSize = 32 * 1024;

export class PublicRelayIngress {
  private server: net.Server | null = null;
  private opts: PublicRelayOptions;

  constructor(opts: PublicRelayOptions) {
    this.opts = opts;
  }

  start(): void {
    if (this.opts.port <= 0) {
      this.opts.logger?.("Public relay ingress disabled (port <= 0)");
      return;
    }

    this.server = net.createServer((socket) => {
      this.handleIncoming(socket).catch((err) => {
        this.opts.logger?.("Public relay ingress connection handler error", { err: String(err) });
        try { socket.destroy(); } catch {}
      });
    });

    this.server.listen(this.opts.port, this.opts.host, () => {
      this.opts.logger?.(`ACBH public relay ingress listening on ${this.opts.host}:${this.opts.port} (players connect here)`);
    });

    this.server.on("error", (err) => {
      this.opts.logger?.("Public relay ingress server error", { err: String(err) });
    });
  }

  stop(): void {
    if (this.server) {
      this.server.close();
      this.server = null;
    }
  }

  private async handleIncoming(tcpConn: net.Socket): Promise<void> {
    const store = this.opts.store as any;
    const listed = typeof store.listGroups === "function" ? store.listGroups() : [];
    const groups = listed.length > 0
      ? listed
      : Array.from(store.groups?.values?.() ?? []);
    if (groups.length === 0) {
      tcpConn.end("No groups configured\n");
      return;
    }

    const activeGroup = groups.find((candidate: any) => Boolean(candidate.currentHostId));
    const group = activeGroup ?? groups[0];
    const groupId = group.groupId;

    if (!group.currentHostId) {
      tcpConn.end("No current host available for relay\n");
      return;
    }

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
    const bufSize = defaultBufferSize;
    let closed = false;
    const closeAll = () => {
      if (closed) return;
      closed = true;
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
}
