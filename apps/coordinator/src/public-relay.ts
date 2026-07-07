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

const defaultBufferSize = 32 * 1024;
const tunnelAttachTimeoutMs = 15_000;

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
        this.opts.logger?.("Public relay ingress connection handler error", { err: String(err), event: "error reason" });
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

  private log(event: string, meta: Record<string, unknown> = {}): void {
    this.opts.logger?.(event, { event, ...meta });
  }

  private async handleIncoming(tcpConn: net.Socket): Promise<void> {
    const remoteAddress = `${tcpConn.remoteAddress ?? "unknown"}:${tcpConn.remotePort ?? 0}`;
    this.log("public connection accepted", { remotePlayerAddress: remoteAddress });

    const groups = this.opts.store.listGroups();
    if (groups.length === 0) {
      this.log("no active tunnel", { reason: "no groups configured", remotePlayerAddress: remoteAddress });
      tcpConn.end("No groups configured\n");
      return;
    }

    const group = selectPublicRelayGroup(groups, new Date(), this.opts.store.heartbeatTimeoutMs);
    if (group === null) {
      this.log("no active tunnel", { reason: "no current host available", remotePlayerAddress: remoteAddress });
      tcpConn.end("No current host available for relay\n");
      return;
    }
    const groupId = group.groupId;
    const currentHost = group.currentHostId === null
      ? undefined
      : group.hosts.find((host) => host.hostId === group.currentHostId);

    this.log("selected relay group", {
      groupId,
      instanceId: groupId,
      deviceId: currentHost?.hostId,
      remotePlayerAddress: remoteAddress,
    });

    if (currentHost?.hostId && !this.opts.relay.hasHostClient(groupId, currentHost.hostId)) {
      this.log("waiting for relay client", {
        groupId,
        deviceId: currentHost.hostId,
        remotePlayerAddress: remoteAddress,
      });
    }

    let playerSess: any;
    try {
      playerSess = this.opts.store.createPlayerSession({ groupId, displayName: "public-mc-player" });
    } catch (e) {
      this.log("error reason", { stage: "create player session", err: String(e), remotePlayerAddress: remoteAddress });
      tcpConn.destroy();
      return;
    }

    let tun: any;
    try {
      tun = this.opts.store.createTunnelSession({ groupId, playerId: playerSess.playerId });
    } catch (e) {
      this.log("error reason", { stage: "create tunnel session", err: String(e), remotePlayerAddress: remoteAddress });
      tcpConn.destroy();
      return;
    }

    const sessionId = tun.sessionId;
    this.opts.relay.setRemotePlayerAddress(sessionId, remoteAddress, groupId);
    this.log("tunnel session created", {
      groupId,
      sessionId,
      deviceId: tun.hostId,
      remotePlayerAddress: remoteAddress,
    });

    const wsURL = this.buildWSURL(groupId, sessionId);
    const headers: Record<string, string> = {};
    headers["X-ACBH-Player-ID"] = playerSess.playerId;
    headers["X-ACBH-Player-Token"] = playerSess.playerToken;

    let ws: WebSocket;
    try {
      ws = new WebSocket(wsURL, { headers });
      await new Promise<void>((resolve, reject) => {
        const t = setTimeout(() => reject(new Error("player WS open timeout")), tunnelAttachTimeoutMs);
        ws.once("open", () => { clearTimeout(t); resolve(); });
        ws.once("error", (e) => { clearTimeout(t); reject(e); });
        ws.once("close", () => { clearTimeout(t); reject(new Error("closed before open")); });
      });
    } catch (e) {
      this.log("timeout waiting for tunnel", {
        sessionId,
        err: String(e),
        remotePlayerAddress: remoteAddress,
        closeReason: "timeout_waiting_for_player_ws",
      });
      tcpConn.destroy();
      return;
    }

    this.log("player side attached", {
      groupId,
      sessionId,
      remotePlayerAddress: remoteAddress,
    });

    const pendingTcpChunks: Buffer[] = [];
    let bytesPlayerToLocal = 0;
    let bytesLocalToPlayer = 0;
    let upstreamCopyStarted = false;
    let downstreamCopyStarted = false;
    let closed = false;

    const closeAll = (reason: string, err?: string) => {
      if (closed) return;
      closed = true;
      const pair = this.opts.relay.getPair(sessionId);
      this.log("forwarding closed", {
        groupId,
        sessionId,
        reason,
        error: err,
        remotePlayerAddress: remoteAddress,
        playerAttached: pair?.playerAttached ?? false,
        hostAttached: pair?.hostAttached ?? false,
        bytesPlayerToLocal,
        bytesLocalToPlayer,
        upstreamCopyStarted,
        downstreamCopyStarted,
        closeInitiator: "public-relay",
      });
      try { tcpConn.destroy(); } catch {}
      try { if (ws.readyState === WebSocket.OPEN) ws.close(1000, "public relay close"); } catch {}
    };

    ws.on("message", (data: Buffer) => {
      if (closed || tcpConn.destroyed) return;
      const chunk = Buffer.from(data);
      tcpConn.write(chunk);
      bytesLocalToPlayer += chunk.length;
    });
    ws.on("close", () => closeAll("player websocket closed"));
    ws.on("error", (e) => closeAll("player websocket error", String(e)));

    tcpConn.on("data", (chunk: Buffer) => {
      if (closed) return;
      if (!upstreamCopyStarted) {
        pendingTcpChunks.push(Buffer.from(chunk));
        return;
      }
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(chunk);
        bytesPlayerToLocal += chunk.length;
      }
    });
    tcpConn.on("close", () => closeAll("player tcp closed"));
    tcpConn.on("error", (e) => closeAll("player tcp error", String(e)));

    const bridge = await this.opts.relay.waitForBridge(sessionId, tunnelAttachTimeoutMs, (waitingFor) => {
      this.log(`waiting for ${waitingFor} side`, {
        sessionId,
        groupId,
        remotePlayerAddress: remoteAddress,
        waitingFor,
      });
    });

    if (!bridge.ok) {
      closeAll(bridge.reason ?? "timeout_waiting_for_peer");
      return;
    }

    if (currentHost?.hostId && this.opts.relay.hasHostClient(groupId, currentHost.hostId)) {
      this.log("host relay client attached", { groupId, sessionId, deviceId: currentHost.hostId });
    }

    upstreamCopyStarted = true;
    downstreamCopyStarted = true;
    this.log("bidirectional bridge started", {
      groupId,
      sessionId,
      remotePlayerAddress: remoteAddress,
      copyStartedUpstream: upstreamCopyStarted,
      copyStartedDownstream: downstreamCopyStarted,
      bytesPlayerToLocal,
      bytesLocalToPlayer,
    });

    for (const chunk of pendingTcpChunks) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(chunk);
        bytesPlayerToLocal += chunk.length;
      }
    }
    pendingTcpChunks.length = 0;
  }

  private buildWSURL(groupId: string, sessionId: string): string {
    let base = this.opts.coordinatorBaseURL || "http://127.0.0.1:6121";
    base = base.replace(/^https:/, "wss:").replace(/^http:/, "ws:");
    if (base.endsWith("/")) base = base.slice(0, -1);
    return `${base}/v1/groups/${groupId}/relay/tunnel-sessions/${sessionId}/player`;
  }
}
