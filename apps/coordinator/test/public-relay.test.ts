import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import * as net from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { WebSocket } from "ws";

import { buildApp } from "../src/app.js";
import { PublicRelayIngress, selectPublicRelayGroup } from "../src/public-relay.js";
import { RelayManager } from "../src/relay.js";
import { LocalFilesystemStorage } from "../src/storage/index.js";
import {
  InMemoryCoordinatorStore,
  createInMemoryCoordinatorStore,
  type PublicGroupState,
} from "../src/store.js";

type HostInfo = { groupId: string; hostId: string; hostToken: string };

test("public relay selection reports no group separately from no current host", () => {
  assert.equal(selectPublicRelayGroup([], new Date("2026-06-24T00:00:00.000Z")), null);

  const store = createInMemoryCoordinatorStore();
  makeHost(store, "idle");
  assert.equal(selectPublicRelayGroup(store.listGroups(), new Date("2026-06-24T00:00:00.000Z")), null);
});

test("public relay selects a single group with fresh current host", () => {
  const now = new Date("2026-06-24T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: () => now });
  const host = makeHost(store, "active");
  makeCurrent(store, host, "hosting");

  const selected = selectPublicRelayGroup(store.listGroups(), now, store.heartbeatTimeoutMs);

  assert.equal(selected?.groupId, host.groupId);
});

test("public relay selects the only group that has a current host", () => {
  const now = new Date("2026-06-24T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: () => now });
  makeHost(store, "idle");
  const active = makeHost(store, "active");
  makeCurrent(store, active, "hosting");

  const selected = selectPublicRelayGroup(store.listGroups(), now, store.heartbeatTimeoutMs);

  assert.equal(selected?.groupId, active.groupId);
});

test("public relay does not let an old stale group preempt a later active group", () => {
  let now = new Date("2026-06-24T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: () => now, heartbeatTimeoutMs: 30_000 });
  const stale = makeHost(store, "old");
  makeCurrent(store, stale, "hosting");

  now = new Date("2026-06-24T00:02:00.000Z");
  const active = makeHost(store, "active");
  makeCurrent(store, active, "hosting");

  const selected = selectPublicRelayGroup(store.listGroups(), now, store.heartbeatTimeoutMs);

  assert.equal(selected?.groupId, active.groupId);
});

test("public relay prefers hosting over standby when both current hosts are fresh", () => {
  const now = new Date("2026-06-24T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: () => now });
  const standby = makeHost(store, "standby");
  makeCurrent(store, standby, "standby");
  const hosting = makeHost(store, "hosting");
  makeCurrent(store, hosting, "hosting");

  const selected = selectPublicRelayGroup(store.listGroups(), now, store.heartbeatTimeoutMs);

  assert.equal(selected?.groupId, hosting.groupId);
});

test("public relay selects active group after store persistence reload", () => {
  let now = new Date("2026-06-24T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: () => now, heartbeatTimeoutMs: 30_000 });
  const stale = makeHost(store, "old");
  makeCurrent(store, stale, "hosting");

  now = new Date("2026-06-24T00:02:00.000Z");
  const active = makeHost(store, "active");
  makeCurrent(store, active, "hosting");

  const reloaded = InMemoryCoordinatorStore.fromSnapshot(store.snapshot(), {
    now: () => now,
    heartbeatTimeoutMs: 30_000,
  });

  const selected = selectPublicRelayGroup(reloaded.listGroups(), now, reloaded.heartbeatTimeoutMs);

  assert.equal(selected?.groupId, active.groupId);
});

test("public relay TCP ingress forwards early downstream bytes and closes the tunnel", async () => {
  const tmpDir = await mkdtemp(path.join(os.tmpdir(), "acbh-public-relay-test-"));
  const storage = new LocalFilesystemStorage(tmpDir);
  const store = createInMemoryCoordinatorStore();
  const relay = new RelayManager(store);
  const app = await buildApp({ store, storage, relay, logger: false });
  const host = makeHost(store, "public-e2e");
  makeCurrent(store, host, "hosting");
  const apiAddress = await app.listen({ port: 0, host: "127.0.0.1" });
  const apiPort = Number(new URL(apiAddress).port);
  const publicPort = await freeTCPPort();
  const earlyServerBytes = Buffer.from("server-ready-before-client-data");
  const ingress = new PublicRelayIngress({
    host: "127.0.0.1",
    port: publicPort,
    coordinatorBaseURL: `http://127.0.0.1:${apiPort}`,
    store,
    relay,
  });
  const stopHostEchoer = startHostSessionEchoer(apiPort, store, host, earlyServerBytes);
  ingress.start();

  try {
    const tcp = await connectTCPWithRetry(publicPort, 1000);

    const early = await readSocketOnce(tcp, 5000);
    assert.deepEqual(early, earlyServerBytes);

    const payload = Buffer.from("client-status-request");
    const echoed = readSocketOnce(tcp, 5000);
    tcp.write(payload);
    assert.deepEqual(await echoed, payload);

    await waitForCondition(() => store.listTunnelSessions(host.groupId).length === 1, 1000);
    const [session] = store.listTunnelSessions(host.groupId);
    assert.ok(session);
    assert.equal(store.getTunnelSession(host.groupId, session.sessionId).status, "active");

    tcp.end();
    await waitForCondition(
      () => store.getTunnelSession(host.groupId, session.sessionId).status === "closed",
      5000,
    );
  } finally {
    stopHostEchoer();
    ingress.stop();
    await app.close();
    await rm(tmpDir, { recursive: true, force: true });
  }
});

function makeHost(store: InMemoryCoordinatorStore, name: string): HostInfo {
  const group = store.createGroup({ name, ownerName: "owner" });
  const host = store.registerHost({
    groupId: group.groupId,
    accessKey: group.accessKey,
    memberId: group.ownerMemberId,
    deviceName: name,
    platform: "test",
    agentVersion: "v0.3.5-hotfix1",
  });
  return { groupId: group.groupId, hostId: host.hostId, hostToken: host.hostToken };
}

function makeCurrent(store: InMemoryCoordinatorStore, host: HostInfo, status: "hosting" | "standby"): PublicGroupState {
  store.updateHeartbeat({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    status: "standby",
    hostScoreHints: { javaAvailable: true },
  });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignment = store.pollTakeover({ groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken }).assignment;
  assert.ok(election.assignment);
  assert.ok(assignment?.takeoverToken);
  store.acceptTakeover({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    assignmentId: election.assignment.assignmentId,
    takeoverToken: assignment.takeoverToken,
  });
  store.completeTakeover({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    assignmentId: election.assignment.assignmentId,
    takeoverToken: assignment.takeoverToken,
  });
  store.updateHeartbeat({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    status,
    hostScoreHints: { javaAvailable: true },
  });
  return store.getGroupState(host.groupId);
}

function startHostSessionEchoer(
  apiPort: number,
  store: InMemoryCoordinatorStore,
  host: HostInfo,
  earlyServerBytes: Buffer,
): () => void {
  const connected = new Set<string>();
  const sockets = new Set<WebSocket>();
  const timer = setInterval(() => {
    for (const session of store.listTunnelSessions(host.groupId)) {
      if (connected.has(session.sessionId)) continue;
      if (session.status !== "pending" && session.status !== "active") continue;
      connected.add(session.sessionId);
      const generation = store.getGroupState(host.groupId).currentHostGeneration;
      const ws = new WebSocket(
        `ws://127.0.0.1:${apiPort}/v1/groups/${host.groupId}/relay/tunnel-sessions/${session.sessionId}/host`,
        { headers: hostHeaders(host, generation) },
      );
      sockets.add(ws);
      ws.on("open", () => {
        ws.send(earlyServerBytes);
      });
      ws.on("message", (data: Buffer) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(Buffer.from(data));
        }
      });
      ws.on("close", () => sockets.delete(ws));
      ws.on("error", () => sockets.delete(ws));
    }
  }, 25);
  return () => {
    clearInterval(timer);
    for (const ws of sockets) {
      try { ws.close(); } catch {}
    }
  };
}

function hostHeaders(host: HostInfo, generation: number): Record<string, string> {
  return {
    "x-acbh-host-id": host.hostId,
    "x-acbh-host-token": host.hostToken,
    "x-acbh-host-generation": String(generation),
  };
}

async function freeTCPPort(): Promise<number> {
  const server = net.createServer();
  server.listen(0, "127.0.0.1");
  await onceServer(server, "listening");
  const address = server.address();
  if (address === null || typeof address === "string") {
    server.close();
    throw new Error("free port lookup did not return a TCP address");
  }
  const port = address.port;
  server.close();
  await onceServer(server, "close");
  return port;
}

function readSocketOnce(socket: net.Socket, timeoutMs: number): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error("timed out waiting for TCP data"));
    }, timeoutMs);
    const onData = (chunk: Buffer) => {
      cleanup();
      resolve(Buffer.from(chunk));
    };
    const onError = (err: Error) => {
      cleanup();
      reject(err);
    };
    const cleanup = () => {
      clearTimeout(timer);
      socket.off("data", onData);
      socket.off("error", onError);
    };
    socket.once("data", onData);
    socket.once("error", onError);
  });
}

function onceSocket(socket: net.Socket, event: "connect" | "close"): Promise<void> {
  return new Promise((resolve, reject) => {
    socket.once(event, () => resolve());
    socket.once("error", reject);
  });
}

async function connectTCPWithRetry(port: number, timeoutMs: number): Promise<net.Socket> {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  while (Date.now() < deadline) {
    const socket = net.createConnection({ host: "127.0.0.1", port });
    try {
      await onceSocket(socket, "connect");
      return socket;
    } catch (err) {
      lastError = err;
      socket.destroy();
      await sleep(25);
    }
  }
  throw lastError instanceof Error ? lastError : new Error("timed out connecting to public relay");
}

function onceServer(server: net.Server, event: "listening" | "close"): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once(event, () => resolve());
    server.once("error", reject);
  });
}

async function waitForCondition(fn: () => boolean, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fn()) return;
    await sleep(25);
  }
  throw new Error("condition not met before timeout");
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
