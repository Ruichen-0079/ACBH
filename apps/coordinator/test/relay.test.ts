import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { WebSocket } from "ws";
import { buildApp } from "../src/app.js";
import {
  createInMemoryCoordinatorStore,
} from "../src/store.js";
import { RelayManager } from "../src/relay.js";
import { LocalFilesystemStorage } from "../src/storage/index.js";

test("host relay connection rejected when tunnel session does not exist", async () => {
  const ctx = await setupRelayTest();
  try {
    const { port, host } = ctx;
    const ws = connectRelayWS(port, host.groupId, "tun_nonexistent", "host",
      { "x-acbh-host-id": host.hostId, "x-acbh-host-token": host.hostToken, "x-acbh-host-generation": "1" });
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("does not exist"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("player relay connection rejected when tunnel session does not exist", async () => {
  const ctx = await setupRelayTest();
  try {
    const { port, groupId } = ctx;
    const ws = connectRelayWS(port, groupId, "tun_nonexistent", "player",
      { "x-acbh-player-id": "plyr_fake", "x-acbh-player-token": "pt_fake" });
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("does not exist"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("host relay connection rejected when host is not current host", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const group = store.createGroup({ name: "Multi", ownerName: "Owner" });
    const accessKey = group.accessKey;

    const hostA = joinAndRegister(store, group.groupId, accessKey, "host-a");
    const hostB = joinAndRegister(store, group.groupId, accessKey, "host-b");

    heartbeat(store, hostA, "standby");
    heartbeat(store, hostB, "standby");
    store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);
    completeTakeover(store, hostA);

    const player = store.createPlayerSession({ groupId: hostA.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: hostA.groupId, playerId: player.playerId });

    const state = store.getGroupState(hostA.groupId);
    const ws = connectRelayWS(port, hostA.groupId, tunnel.sessionId, "host",
      hostHeaders(hostB, state.currentHostGeneration));
    const closeMsg = await waitForClose(ws);
    assert.ok(
      closeMsg.includes("Only the current host") || closeMsg.includes("different host"),
      `Got: ${closeMsg}`,
    );
  } finally {
    await ctx.close();
  }
});

test("host relay connection rejected on stale generation", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const ws = connectRelayWS(port, host.groupId, tunnel.sessionId, "host",
      hostHeaders(host, 999));
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("stale") || closeMsg.includes("generation"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("player relay connection rejected with invalid player credentials", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const ws = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": "pt_wrong_token" });
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("Invalid player token"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("duplicate host connection for same session rejected", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const ws1 = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(ws1);
    await sleep(50);

    const ws2 = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    const closeMsg = await waitForClose(ws2);
    assert.ok(closeMsg.includes("already connected"), `Got: ${closeMsg}`);

    ws1.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("duplicate player connection for same session rejected", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const ws1 = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(ws1);
    await sleep(50);

    const ws2 = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    const closeMsg = await waitForClose(ws2);
    assert.ok(closeMsg.includes("already connected"), `Got: ${closeMsg}`);

    ws1.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("host binary frame reaches player", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    const testData = Buffer.from([0x01, 0x02, 0x03, 0x04, 0x05]);
    const received = await forwardAndReceive(playerWs, hostWs, testData);
    assert.deepEqual(received, testData);

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("player binary frame reaches host", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    const testData = Buffer.from([0xAA, 0xBB, 0xCC]);
    const received = await forwardAndReceive(hostWs, playerWs, testData);
    assert.deepEqual(received, testData);

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("multiple frames preserve order", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    const messages: Buffer[] = [];
    playerWs.on("message", (data: Buffer) => messages.push(Buffer.from(data)));

    const frame1 = Buffer.from([0x01, 0x02]);
    const frame2 = Buffer.from([0x03, 0x04, 0x05]);
    const frame3 = Buffer.from([0x06]);
    hostWs.send(frame1);
    hostWs.send(frame2);
    hostWs.send(frame3);
    await sleep(100);

    assert.equal(messages.length, 3);
    assert.deepEqual(messages[0], frame1);
    assert.deepEqual(messages[1], frame2);
    assert.deepEqual(messages[2], frame3);

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("closing host side closes the relay pair", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    const playerClose = waitForClose(playerWs);
    hostWs.close(1000, "Normal close");
    const closeMsg = await playerClose;
    assert.ok(closeMsg.includes("Host disconnected"), `Got: ${closeMsg}`);

    await sleep(100);
    assert.equal(store.getTunnelSession(host.groupId, tunnel.sessionId).status, "closed");
  } finally {
    await ctx.close();
  }
});

test("closing player side closes the relay pair", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    const hostClose = waitForClose(hostWs);
    playerWs.close(1000, "Normal close");
    const closeMsg = await hostClose;
    assert.ok(closeMsg.includes("Player disconnected"), `Got: ${closeMsg}`);

    await sleep(100);
    assert.equal(store.getTunnelSession(host.groupId, tunnel.sessionId).status, "closed");
  } finally {
    await ctx.close();
  }
});

test("tunnel status becomes active when both sides connect", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });
    assert.equal(tunnel.status, "pending");

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);
    assert.equal(store.getTunnelSession(host.groupId, tunnel.sessionId).status, "pending");

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);
    assert.equal(store.getTunnelSession(host.groupId, tunnel.sessionId).status, "active");

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("tunnel status becomes closed when relay closes", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);

    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    hostWs.close(1000, "Done");
    playerWs.close(1000, "Done");
    await sleep(100);

    assert.equal(store.getTunnelSession(host.groupId, tunnel.sessionId).status, "closed");
  } finally {
    await ctx.close();
  }
});

test("relay runtime state not in persisted snapshot", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);
    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    assert.equal(store.getTunnelSession(host.groupId, tunnel.sessionId).status, "active");

    const snapshotJson = JSON.stringify(store.snapshot());
    assert.equal(snapshotJson.includes("RelayPair"), false);
    assert.equal(snapshotJson.includes("bytesHostToPlayer"), false);

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("player token returned on creation but not on getPlayerSession", () => {
  const store = createInMemoryCoordinatorStore();
  const host = makeHost(store, "host");
  const created = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  assert.ok(created.playerToken?.startsWith("pt_"));

  const fetched = store.getPlayerSession(host.groupId, created.playerId);
  assert.equal((fetched as Record<string, unknown>).playerToken, undefined);
});

test("createPlayerSession tokens are unique", () => {
  const store = createInMemoryCoordinatorStore();
  const host = makeHost(store, "host");
  const p1 = store.createPlayerSession({ groupId: host.groupId, displayName: "A" });
  const p2 = store.createPlayerSession({ groupId: host.groupId, displayName: "B" });
  assert.notEqual(p1.playerToken, p2.playerToken);
});

test("verifyPlayerToken rejects wrong token", () => {
  const store = createInMemoryCoordinatorStore();
  const host = makeHost(store, "host");
  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });

  store.verifyPlayerToken(host.groupId, player.playerId, player.playerToken!);
  assert.throws(
    () => store.verifyPlayerToken(host.groupId, player.playerId, "pt_wrong"),
    /Invalid player token/,
  );
});

test("player relay rejected when playerId mismatch", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const p1 = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const p2 = store.createPlayerSession({ groupId: host.groupId, displayName: "Alex" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: p1.playerId });

    const ws = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": p2.playerId, "x-acbh-player-token": p2.playerToken! });
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("does not belong"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("closed tunnel rejected for relay", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });
    store.updateTunnelSessionStatus(host.groupId, tunnel.sessionId, "closed");

    const ws = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("closed") || closeMsg.includes("cannot be joined"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("player relay rejected without headers", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const ws = connectRelayWS(port, host.groupId, tunnel.sessionId, "player", {});
    const closeMsg = await waitForClose(ws);
    assert.ok(closeMsg.includes("required"), `Got: ${closeMsg}`);
  } finally {
    await ctx.close();
  }
});

test("bytes forwarded counter tracks transfers", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port, relay } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);
    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    await forwardAndReceive(playerWs, hostWs, Buffer.alloc(100, 0xAB));
    await forwardAndReceive(hostWs, playerWs, Buffer.alloc(50, 0xCD));
    await sleep(50);

    const pair = relay.getPair(tunnel.sessionId);
    assert.ok(pair);
    assert.equal(pair.bytesHostToPlayer, 100);
    assert.equal(pair.bytesPlayerToHost, 50);

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

test("tunnel session response does not expose sensitive tokens", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;

    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
    const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

    const hostWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "host", hostHeaders(host, 1));
    await waitForOpen(hostWs);
    await sleep(50);
    const playerWs = connectRelayWS(port, host.groupId, tunnel.sessionId, "player",
      { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! });
    await waitForOpen(playerWs);
    await sleep(100);

    const tunnelJson = JSON.stringify(store.getTunnelSession(host.groupId, tunnel.sessionId));
    assert.equal(tunnelJson.includes(host.hostToken), false);
    assert.equal(tunnelJson.includes(player.playerToken!), false);

    hostWs.close();
    playerWs.close();
    await sleep(50);
  } finally {
    await ctx.close();
  }
});

//
// Helpers
//

interface TestContext {
  app: Awaited<ReturnType<typeof buildApp>>;
  store: ReturnType<typeof createInMemoryCoordinatorStore>;
  relay: RelayManager;
  storage: LocalFilesystemStorage;
  port: number;
  groupId: string;
  host: HostInfo;
  close: () => Promise<void>;
}

type HostInfo = { groupId: string; hostId: string; hostToken: string; hostGeneration: number };

async function setupRelayTest(): Promise<TestContext> {
  const tmpDir = await mkdtemp(path.join(os.tmpdir(), "acbh-relay-test-"));
  const storage = new LocalFilesystemStorage(tmpDir);
  const store = createInMemoryCoordinatorStore();

  const group = store.createGroup({ name: "RelayTest", ownerName: "Owner" });
  const joined = store.joinGroup({ groupId: group.groupId, accessKey: group.accessKey, displayName: "Host" });
  const hostReg = store.registerHost({ groupId: group.groupId, accessKey: group.accessKey, memberId: joined.memberId, deviceName: "host", platform: "test", agentVersion: "0.1.0" });

  const host: HostInfo = { groupId: group.groupId, hostId: hostReg.hostId, hostToken: hostReg.hostToken, hostGeneration: 1 };

  const relay = new RelayManager(store);
  const app = await buildApp({ store, storage, relay, logger: false });
  const address = await app.listen({ port: 0, host: "127.0.0.1" });
  const port = Number(new URL(address).port);

  return {
    app, store, relay, storage, port,
    groupId: group.groupId, host,
    close: async () => { await app.close(); await rm(tmpDir, { recursive: true, force: true }); },
  };
}

function makeHost(store: ReturnType<typeof createInMemoryCoordinatorStore>, name: string): HostInfo {
  const group = store.createGroup({ name: `Test-${name}`, ownerName: "Owner" });
  const joined = store.joinGroup({ groupId: group.groupId, accessKey: group.accessKey, displayName: name });
  const host = store.registerHost({ groupId: group.groupId, accessKey: group.accessKey, memberId: joined.memberId, deviceName: name, platform: "test", agentVersion: "0.1.0" });
  return { groupId: group.groupId, hostId: host.hostId, hostToken: host.hostToken, hostGeneration: 1 };
}

function joinAndRegister(store: ReturnType<typeof createInMemoryCoordinatorStore>, groupId: string, accessKey: string, name: string): HostInfo {
  const joined = store.joinGroup({ groupId, accessKey, displayName: name });
  const host = store.registerHost({ groupId, accessKey, memberId: joined.memberId, deviceName: name, platform: "test", agentVersion: "0.1.0" });
  return { groupId, hostId: host.hostId, hostToken: host.hostToken, hostGeneration: 1 };
}

function heartbeat(store: ReturnType<typeof createInMemoryCoordinatorStore>, host: HostInfo, status: "online" | "standby" | "hosting" | "unhealthy" | "offline"): void {
  store.updateHeartbeat({ groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken, status, hostScoreHints: { javaAvailable: true } });
}

function completeTakeover(store: ReturnType<typeof createInMemoryCoordinatorStore>, host: HostInfo): void {
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const aId = election.assignment?.assignmentId;
  if (!aId) throw new Error("no assignment");
  const poll = store.pollTakeover(host);
  const token = poll.assignment?.takeoverToken;
  if (!token) throw new Error("no token");
  store.acceptTakeover({ ...host, assignmentId: aId, takeoverToken: token });
  store.completeTakeover({ ...host, assignmentId: aId, takeoverToken: token });
}

function hostHeaders(host: HostInfo, generation: number): Record<string, string> {
  return { "x-acbh-host-id": host.hostId, "x-acbh-host-token": host.hostToken, "x-acbh-host-generation": String(generation) };
}

test("relay host client websocket registers and stays open", async () => {
  const ctx = await setupRelayTest();
  try {
    const { store, port } = ctx;
    const host = makeHost(store, "host");
    heartbeat(store, host, "standby");
    completeTakeover(store, host);

    const url = `ws://127.0.0.1:${port}/v1/groups/${host.groupId}/relay/clients/host`;
    const ws = new WebSocket(url, { headers: hostHeaders(host, 1) });
    await waitForOpen(ws);
    await sleep(100);
    ws.close();
    await waitForClose(ws);
  } finally {
    await ctx.close();
  }
});

function connectRelayWS(port: number, groupId: string, sessionId: string, side: string, headers: Record<string, string>): WebSocket {
  const url = `ws://127.0.0.1:${port}/v1/groups/${groupId}/relay/tunnel-sessions/${sessionId}/${side}`;
  return new WebSocket(url, { headers });
}

function waitForOpen(ws: WebSocket): Promise<void> {
  return new Promise((resolve) => { ws.on("open", resolve); });
}

function waitForClose(ws: WebSocket): Promise<string> {
  return new Promise((resolve) => {
    setTimeout(() => resolve("timeout"), 3000);
    ws.on("close", (_code, reason) => resolve(reason.toString()));
  });
}

function forwardAndReceive(target: WebSocket, sender: WebSocket, data: Buffer): Promise<Buffer> {
  return new Promise((resolve) => {
    target.once("message", (d: Buffer) => resolve(Buffer.from(d)));
    sender.send(data);
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
