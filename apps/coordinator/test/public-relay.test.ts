import assert from "node:assert/strict";
import * as net from "node:net";
import test from "node:test";
import { WebSocket } from "ws";

import { buildApp } from "../src/app.js";
import { PublicRelayIngress, selectPublicRelayGroup } from "../src/public-relay.js";
import { RelayManager } from "../src/relay.js";
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

test("public relay start opens TCP listener and forwards bytes through host websocket", async (t) => {
  const apiPort = await freePort();
  const publicPort = await freePort();
  const oldPort = process.env.PORT;
  process.env.PORT = String(apiPort);
  t.after(() => {
    if (oldPort === undefined) {
      delete process.env.PORT;
    } else {
      process.env.PORT = oldPort;
    }
  });

  const store = createInMemoryCoordinatorStore();
  const host = makeHost(store, "relay-host");
  makeCurrent(store, host, "hosting");
  const app = await buildApp({ store, logger: false });
  await app.listen({ host: "127.0.0.1", port: apiPort });
  t.after(async () => {
    (app as any).publicRelay?.stop();
    await app.close();
  });

  const started = await app.inject({
    method: "POST",
    url: "/v1/public-relay/start",
    payload: { groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken, publicPort },
  });
  assert.equal(started.statusCode, 200, started.body);
  assert.equal(started.json().relay.publicListenerActive, true);

  const tcp = net.createConnection({ host: "127.0.0.1", port: publicPort });
  t.after(() => tcp.destroy());
  await onceEvent(tcp, "connect", 5_000);

  const session = await waitFor(() => store.listTunnelSessions(host.groupId)[0]);
  assert.equal(session.hostId, host.hostId);

  const ws = new WebSocket(
    `ws://127.0.0.1:${apiPort}/v1/groups/${host.groupId}/relay/tunnel-sessions/${session.sessionId}/host`,
    {
      headers: {
        "X-ACBH-Host-ID": host.hostId,
        "X-ACBH-Host-Token": host.hostToken,
        "X-ACBH-Host-Generation": String(session.currentHostGeneration),
      },
    },
  );
  t.after(() => ws.close());
  await onceEvent(ws, "open", 5_000);

  tcp.write(Buffer.from("ping"));
  const hostData = await onceMessage(ws);
  assert.equal(hostData.toString(), "ping");

  ws.send(Buffer.from("pong"));
  const playerData = await onceData(tcp);
  assert.equal(playerData.toString(), "pong");

  const active = await app.inject({ method: "GET", url: "/v1/public-relay/status" });
  assert.equal(active.statusCode, 200);
  assert.equal(active.json().activeConnections, 1);

  const stopped = await app.inject({
    method: "POST",
    url: "/v1/public-relay/stop",
    payload: { groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken },
  });
  assert.equal(stopped.statusCode, 200, stopped.body);
  assert.equal(stopped.json().relay.publicListenerActive, false);
  ws.close();
  tcp.destroy();
});

test("public relay forwards client-first TCP payload through host websocket to local echo server", async (t) => {
  const apiPort = await freePort();
  const publicPort = await freePort();
  const oldPort = process.env.PORT;
  process.env.PORT = String(apiPort);
  t.after(() => {
    if (oldPort === undefined) {
      delete process.env.PORT;
    } else {
      process.env.PORT = oldPort;
    }
  });

  const store = createInMemoryCoordinatorStore();
  const host = makeHost(store, "relay-client-first-host");
  makeCurrent(store, host, "hosting");
  const app = await buildApp({ store, logger: false });
  await app.listen({ host: "127.0.0.1", port: apiPort });
  t.after(async () => {
    (app as any).publicRelay?.stop();
    await app.close();
  });

  const echo = await startEchoServer(t);
  const started = await app.inject({
    method: "POST",
    url: "/v1/public-relay/start",
    headers: { host: "121.40.101.224:6121" },
    payload: { groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken, publicPort },
  });
  assert.equal(started.statusCode, 200, started.body);
  assert.equal(started.json().relay.publicEndpoint, `121.40.101.224:${publicPort}`);

  const tcp = net.createConnection({ host: "127.0.0.1", port: publicPort });
  t.after(() => tcp.destroy());
  await onceEvent(tcp, "connect", 5_000);

  const payload = Buffer.from([0x00, 0x0f, 0x01, 0x70, 0x69, 0x6e, 0x67]);
  tcp.write(payload);

  const session = await waitFor(() => store.listTunnelSessions(host.groupId)[0]);
  const bridge = await connectHostBridge(t, apiPort, host, session.sessionId, session.currentHostGeneration, echo);
  t.after(() => {
    bridge.ws.close();
    bridge.local.destroy();
  });

  const echoed = await onceData(tcp);
  assert.deepEqual(echoed, payload);

  const active = await waitFor(async () => {
    const status = await app.inject({
      method: "GET",
      url: "/v1/public-relay/status",
      headers: { host: "121.40.101.224:6121" },
    });
    assert.equal(status.statusCode, 200, status.body);
    const body = status.json();
    const debug = body.recentConnections?.[0];
    if (
      debug?.bytesPlayerToCoordinator >= payload.length &&
      debug?.bytesCoordinatorToHost >= payload.length &&
      debug?.bytesCoordinatorToPlayer >= payload.length
    ) {
      return body;
    }
    return undefined;
  });
  assert.equal(active.publicEndpoint, `121.40.101.224:${publicPort}`);
  assert.equal(active.recentConnections[0].sessionId, session.sessionId);
  assert.equal(active.recentConnections[0].hostConnected, true);

  tcp.destroy();
  bridge.ws.close();
  bridge.local.destroy();
  const inactive = await waitFor(() => {
    const status = (app as any).publicRelay.status("121.40.101.224");
    return status.activeConnections === 0 ? status : undefined;
  }, 2_000);
  assert.equal(inactive.activeConnections, 0);
});

test("public relay forwards concurrent client-first TCP clients", async (t) => {
  const apiPort = await freePort();
  const publicPort = await freePort();
  const oldPort = process.env.PORT;
  process.env.PORT = String(apiPort);
  t.after(() => {
    if (oldPort === undefined) {
      delete process.env.PORT;
    } else {
      process.env.PORT = oldPort;
    }
  });

  const store = createInMemoryCoordinatorStore();
  const host = makeHost(store, "relay-concurrent-host");
  makeCurrent(store, host, "hosting");
  const app = await buildApp({ store, logger: false });
  await app.listen({ host: "127.0.0.1", port: apiPort });
  t.after(async () => {
    (app as any).publicRelay?.stop();
    await app.close();
  });

  const echo = await startEchoServer(t);
  const started = await app.inject({
    method: "POST",
    url: "/v1/public-relay/start",
    payload: { groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken, publicPort },
  });
  assert.equal(started.statusCode, 200, started.body);

  const clients = [
    { socket: net.createConnection({ host: "127.0.0.1", port: publicPort }), payload: Buffer.from("alpha") },
    { socket: net.createConnection({ host: "127.0.0.1", port: publicPort }), payload: Buffer.from("bravo") },
  ];
  for (const client of clients) {
    t.after(() => client.socket.destroy());
  }
  await Promise.all(clients.map((client) => onceEvent(client.socket, "connect", 5_000)));
  for (const client of clients) {
    client.socket.write(client.payload);
  }

  const sessions = await waitFor(() => {
    const list = store.listTunnelSessions(host.groupId);
    return list.length >= 2 ? list.slice(0, 2) : undefined;
  });
  const bridges = await Promise.all(sessions.map((session) => (
    connectHostBridge(t, apiPort, host, session.sessionId, session.currentHostGeneration, echo)
  )));
  t.after(() => {
    for (const bridge of bridges) {
      bridge.ws.close();
      bridge.local.destroy();
    }
  });

  const echoed = await Promise.all(clients.map((client) => onceData(client.socket)));
  assert.deepEqual(echoed.map((chunk) => chunk.toString()).sort(), ["alpha", "bravo"]);

  for (const client of clients) {
    client.socket.destroy();
  }
  for (const bridge of bridges) {
    bridge.ws.close();
    bridge.local.destroy();
  }
  const inactive = await waitFor(() => {
    const status = (app as any).publicRelay.status("121.40.101.224");
    return status.activeConnections === 0 ? status : undefined;
  }, 2_000);
  assert.equal(inactive.activeConnections, 0);
});

test("public relay stops when the current host heartbeat expires", async (t) => {
  const publicPort = await freePort();
  const store = createInMemoryCoordinatorStore({ heartbeatTimeoutMs: 50 });
  const host = makeHost(store, "relay-stale-host");
  makeCurrent(store, host, "hosting");
  const ingress = new PublicRelayIngress({
    host: "127.0.0.1",
    port: 0,
    coordinatorBaseURL: "http://127.0.0.1:1",
    store,
    relay: new RelayManager(store),
  });
  t.after(() => ingress.stop());

  await ingress.start(publicPort);
  assert.equal(ingress.status().publicListenerActive, true);

  await waitFor(() => {
    const status = ingress.status();
    return status.publicListenerActive ? undefined : status;
  }, 2_000);
  assert.equal(ingress.status().publicListenerActive, false);
  assert.match(ingress.status().lastError ?? "", /no current host heartbeat is fresh/);
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

async function freePort(): Promise<number> {
  return await new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (address === null || typeof address === "string") {
        reject(new Error("no tcp port assigned"));
        return;
      }
      const port = address.port;
      server.close(() => resolve(port));
    });
  });
}

async function waitFor<T>(fn: () => T | undefined, timeoutMs = 5_000): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const value = fn();
    if (value !== undefined) {
      return value;
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("timed out waiting for condition");
}

async function onceEvent(emitter: NodeJS.EventEmitter, event: string, timeoutMs = 5_000): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(`timed out waiting for ${event}`));
    }, timeoutMs);
    const onError = (err: Error) => {
      cleanup();
      reject(err);
    };
    const onEvent = () => {
      cleanup();
      resolve();
    };
    const cleanup = () => {
      clearTimeout(timer);
      emitter.off(event, onEvent);
      emitter.off("error", onError);
    };
    emitter.once(event, onEvent);
    emitter.once("error", onError);
  });
}

async function onceMessage(ws: WebSocket): Promise<Buffer> {
  return await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timed out waiting for websocket message")), 5_000);
    ws.once("message", (data) => {
      clearTimeout(timer);
      resolve(Buffer.isBuffer(data) ? data : Buffer.from(data as ArrayBuffer));
    });
    ws.once("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
  });
}

async function onceData(socket: net.Socket): Promise<Buffer> {
  return await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timed out waiting for tcp data")), 5_000);
    socket.once("data", (data) => {
      clearTimeout(timer);
      resolve(data);
    });
    socket.once("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
  });
}

async function startEchoServer(t: Parameters<Parameters<typeof test>[1]>[0]): Promise<{ host: string; port: number }> {
  const server = net.createServer((socket) => {
    socket.on("data", (chunk) => {
      socket.write(chunk);
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  t.after(() => server.close());
  const address = server.address();
  assert.notEqual(address, null);
  assert.notEqual(typeof address, "string");
  return { host: (address as net.AddressInfo).address, port: (address as net.AddressInfo).port };
}

async function connectHostBridge(
  t: Parameters<Parameters<typeof test>[1]>[0],
  apiPort: number,
  host: HostInfo,
  sessionId: string,
  generation: number,
  localAddress: { host: string; port: number },
): Promise<{ ws: WebSocket; local: net.Socket }> {
  const local = net.createConnection(localAddress);
  await onceEvent(local, "connect", 5_000);
  const ws = new WebSocket(
    `ws://127.0.0.1:${apiPort}/v1/groups/${host.groupId}/relay/tunnel-sessions/${sessionId}/host`,
    {
      headers: {
        "X-ACBH-Host-ID": host.hostId,
        "X-ACBH-Host-Token": host.hostToken,
        "X-ACBH-Host-Generation": String(generation),
      },
    },
  );
  ws.on("message", (data) => {
    local.write(messageDataToBuffer(data));
  });
  local.on("data", (chunk) => {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(chunk, { binary: true });
    }
  });
  t.after(() => {
    ws.close();
    local.destroy();
  });
  await onceEvent(ws, "open", 5_000);
  return { ws, local };
}

function messageDataToBuffer(data: unknown): Buffer {
  if (Buffer.isBuffer(data)) {
    return Buffer.from(data);
  }
  if (Array.isArray(data)) {
    return Buffer.concat(data.map((part) => Buffer.from(part)));
  }
  return Buffer.from(data as ArrayBuffer);
}
