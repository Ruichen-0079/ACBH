import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildApp } from "../src/app.js";
import {
  createInMemoryCoordinatorStore,
  InMemoryCoordinatorStore,
  StoreError,
  type LatestLocalArtifacts,
} from "../src/store.js";
import type { TunnelSession, PlayerSession } from "../src/network.js";
import { LocalFilesystemStorage } from "../src/storage/index.js";

test("cannot create tunnel when group has no current host", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });

  assert.throws(
    () => store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId }),
    /no current host/,
  );
});

test("cannot create tunnel when no takeover has completed (no current host)", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  assert.equal(election.ok, true);

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });

  assert.throws(
    () => store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId }),
    /no current host/,
  );
});

test("tunnel session binds to currentHostId and currentHostGeneration", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const state = store.getGroupState(host.groupId);
  assert.equal(state.currentHostId, host.hostId);
  assert.equal(state.currentHostGeneration, 1);

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  assert.equal(tunnel.hostId, host.hostId);
  assert.equal(tunnel.currentHostGeneration, 1);
  assert.equal(tunnel.mode, "relay");
  assert.equal(tunnel.status, "pending");
  assert.equal(tunnel.playerId, player.playerId);
  assert.equal(tunnel.groupId, host.groupId);
});

test("after takeover, new tunnel sessions target the new current host", () => {
  const store = createInMemoryCoordinatorStore();
  const hostA = createHost(store);
  const hostB = addHost(store, hostA, "host-b");

  heartbeat(store, hostA, { status: "standby", javaAvailable: true });
  heartbeat(store, hostB, { status: "standby", javaAvailable: true });
  store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);

  const election1 = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
  const assignmentId1 = required(election1.assignment?.assignmentId);
  const poll1 = store.pollTakeover(hostA);
  const token1 = required(poll1.assignment?.takeoverToken);
  store.acceptTakeover({ ...hostA, assignmentId: assignmentId1, takeoverToken: token1 });
  store.completeTakeover({ ...hostA, assignmentId: assignmentId1, takeoverToken: token1 });

  const stateAfterA = store.getGroupState(hostA.groupId);
  assert.equal(stateAfterA.currentHostId, hostA.hostId);
  assert.equal(stateAfterA.currentHostGeneration, 1);

  const player = store.createPlayerSession({ groupId: hostA.groupId, displayName: "Steve" });
  const tunnel1 = store.createTunnelSession({ groupId: hostA.groupId, playerId: player.playerId });
  assert.equal(tunnel1.hostId, hostA.hostId);
  assert.equal(tunnel1.currentHostGeneration, 1);

  store.setHostManualPriority(hostA.groupId, hostB.hostId, 40);
  const election2 = store.runElection({ groupId: hostA.groupId, reason: "manual" });
  const assignmentId2 = required(election2.assignment?.assignmentId);
  const poll2 = store.pollTakeover(hostB);
  const token2 = required(poll2.assignment?.takeoverToken);
  store.acceptTakeover({ ...hostB, assignmentId: assignmentId2, takeoverToken: token2 });
  store.completeTakeover({ ...hostB, assignmentId: assignmentId2, takeoverToken: token2 });

  const stateAfterB = store.getGroupState(hostA.groupId);
  assert.equal(stateAfterB.currentHostId, hostB.hostId);
  assert.equal(stateAfterB.currentHostGeneration, 2);

  const tunnel2 = store.createTunnelSession({ groupId: hostA.groupId, playerId: player.playerId });
  assert.equal(tunnel2.hostId, hostB.hostId);
  assert.equal(tunnel2.currentHostGeneration, 2);

  assert.equal(tunnel1.currentHostGeneration, 1);
  assert.equal(tunnel2.currentHostGeneration, 2);
});

test("tunnel session expires", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, tunnelSessionTtlMs: 10_000 });
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });
  assert.equal(tunnel.status, "pending");

  const fetched = store.getTunnelSession(host.groupId, tunnel.sessionId);
  assert.equal(fetched.status, "pending");

  clock.advance(12_000);
  store.expireTunnelSessions(clock.now());

  const expired = store.getTunnelSession(host.groupId, tunnel.sessionId);
  assert.equal(expired.status, "expired");
});

test("player session can be created and fetched", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  const created = store.createPlayerSession({ groupId: host.groupId, displayName: "Alex" });
  assert.equal(created.displayName, "Alex");
  assert.equal(created.groupId, host.groupId);
  assert.ok(created.playerId.startsWith("plyr_"));
  assert.ok(created.createdAt);
  assert.ok(created.expiresAt);

  const fetched = store.getPlayerSession(host.groupId, created.playerId);
  assert.equal(fetched.playerId, created.playerId);
  assert.equal(fetched.displayName, "Alex");
});

test("player session expires and is cleaned up", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, tunnelSessionTtlMs: 5_000 });
  const host = createHost(store);

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  store.getPlayerSession(host.groupId, player.playerId);

  clock.advance(6_000);
  store.expireTunnelSessions(clock.now());

  assert.throws(
    () => store.getPlayerSession(host.groupId, player.playerId),
    /Player session does not exist/,
  );
});

test("tunnel session response does not expose host token or takeover token material", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  const tunnelJson = JSON.stringify(tunnel);
  assert.equal(tunnelJson.includes(host.hostToken), false, "must not expose host token");
  assert.equal(tunnelJson.includes(takeoverToken), false, "must not expose takeover token");
  assert.equal(tunnelJson.includes("token"), false, "must not expose any token material");
  assert.equal(tunnelJson.includes("hostToken"), false);
  assert.equal(tunnelJson.includes("takeoverToken"), false);

  const playerJson = JSON.stringify(player);
  assert.equal(playerJson.includes(host.hostToken), false);
  assert.equal(playerJson.includes("token"), false);
});

test("tunnel session update status works", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  assert.equal(tunnel.status, "pending");

  const updated = store.updateTunnelSessionStatus(host.groupId, tunnel.sessionId, "active");
  assert.equal(updated.status, "active");

  const fetched = store.getTunnelSession(host.groupId, tunnel.sessionId);
  assert.equal(fetched.status, "active");
});

test("persistence snapshot excludes tunnel sessions", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Alex" });
  store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  const snapshot = store.snapshot();

  const snapshotJson = JSON.stringify(snapshot);
  assert.equal(snapshotJson.includes("playerSessions"), false, "snapshot must not include playerSessions");
  assert.equal(snapshotJson.includes("tunnelSessions"), false, "snapshot must not include tunnelSessions");
  assert.equal(snapshotJson.includes("tun_"), false, "snapshot must not include tunnel session IDs");
  assert.equal(snapshotJson.includes("plyr_"), false, "snapshot must not include player session IDs");

  const reloaded = InMemoryCoordinatorStore.fromSnapshot(snapshot);
  const reloadedState = reloaded.getGroupState(host.groupId);
  assert.equal(reloadedState.currentHostId, host.hostId);
  assert.equal(reloadedState.currentHostGeneration, 1);

  assert.throws(
    () => reloaded.getPlayerSession(host.groupId, player.playerId),
    /Player session does not exist/,
  );
});

test("create tunnel session rejects unknown player session", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  assert.throws(
    () => store.createTunnelSession({ groupId: host.groupId, playerId: "plyr_nonexistent" }),
    /Player session does not exist/,
  );
});

test("create tunnel session rejects unknown group", () => {
  const store = createInMemoryCoordinatorStore();

  assert.throws(
    () => store.createTunnelSession({ groupId: "grp_nonexistent", playerId: "plyr_fake" }),
    /Group does not exist/,
  );
});

test("update tunnel session rejects unknown session", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  assert.throws(
    () => store.updateTunnelSessionStatus(host.groupId, "tun_nonexistent", "active"),
    /Tunnel session does not exist/,
  );
});

test("get tunnel session rejects unknown session", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  assert.throws(
    () => store.getTunnelSession(host.groupId, "tun_nonexistent"),
    /Tunnel session does not exist/,
  );
});

test("get player session rejects unknown session", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  assert.throws(
    () => store.getPlayerSession(host.groupId, "plyr_nonexistent"),
    /Player session does not exist/,
  );
});

test("tunnel session TTL respects environment variable", () => {
  const store = createInMemoryCoordinatorStore({ tunnelSessionTtlMs: 123_456 });
  assert.equal(store.tunnelSessionTtlMs, 123_456);
});

test("default tunnel session TTL is 5 minutes", () => {
  const store = createInMemoryCoordinatorStore();
  assert.equal(store.tunnelSessionTtlMs, 300_000);
});

test("updateTunnelSessionStatus handles multiple transitions", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  assert.equal(store.updateTunnelSessionStatus(host.groupId, tunnel.sessionId, "active").status, "active");
  assert.equal(store.updateTunnelSessionStatus(host.groupId, tunnel.sessionId, "closed").status, "closed");
  assert.equal(store.updateTunnelSessionStatus(host.groupId, tunnel.sessionId, "failed").status, "failed");
});

test("player session without display name works", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  const player = store.createPlayerSession({ groupId: host.groupId });
  assert.equal(player.displayName, undefined);
  assert.equal(player.groupId, host.groupId);

  const fetched = store.getPlayerSession(host.groupId, player.playerId);
  assert.equal(fetched.displayName, undefined);
});

test("create tunnel session in nonexistent group throws 404", () => {
  const store = createInMemoryCoordinatorStore();

  assert.throws(
    () => store.createTunnelSession({ groupId: "grp_none", playerId: "plyr_fake" }),
    (err: unknown) => err instanceof StoreError && err.statusCode === 404,
  );
});

test("create player session in nonexistent group throws 404", () => {
  const store = createInMemoryCoordinatorStore();

  assert.throws(
    () => store.createPlayerSession({ groupId: "grp_none" }),
    (err: unknown) => err instanceof StoreError && err.statusCode === 404,
  );
});

test("expireTunnelSessions with no sessions is safe", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now });
  createHost(store);

  store.expireTunnelSessions();
});

test("expireTunnelSessions does not expire sessions that have not reached TTL", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, tunnelSessionTtlMs: 10_000 });
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  clock.advance(5_000);
  store.expireTunnelSessions(clock.now());

  const fetched = store.getTunnelSession(host.groupId, tunnel.sessionId);
  assert.equal(fetched.status, "pending");
});

test("cannot get tunnel session from another group", () => {
  const store = createInMemoryCoordinatorStore();
  const hostA = createHost(store);
  const hostB = createHost(store);

  heartbeat(store, hostA, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(hostA);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...hostA, assignmentId, takeoverToken });
  store.completeTakeover({ ...hostA, assignmentId, takeoverToken });

  heartbeat(store, hostB, { status: "standby", javaAvailable: true });
  const electionB = store.runElection({ groupId: hostB.groupId, reason: "no-current-host" });
  const assignmentIdB = required(electionB.assignment?.assignmentId);
  const pollB = store.pollTakeover(hostB);
  const tokenB = required(pollB.assignment?.takeoverToken);
  store.acceptTakeover({ ...hostB, assignmentId: assignmentIdB, takeoverToken: tokenB });
  store.completeTakeover({ ...hostB, assignmentId: assignmentIdB, takeoverToken: tokenB });

  const player = store.createPlayerSession({ groupId: hostA.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: hostA.groupId, playerId: player.playerId });

  assert.throws(
    () => store.getTunnelSession(hostB.groupId, tunnel.sessionId),
    /Tunnel session does not exist/,
  );
});

test("expireTunnelSessions only expires sessions past TTL", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, tunnelSessionTtlMs: 5_000 });
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player1 = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel1 = store.createTunnelSession({ groupId: host.groupId, playerId: player1.playerId });

  clock.advance(7_000);

  const player2 = store.createPlayerSession({ groupId: host.groupId, displayName: "Alex" });
  const tunnel2 = store.createTunnelSession({ groupId: host.groupId, playerId: player2.playerId });

  store.expireTunnelSessions(clock.now());

  assert.equal(store.getTunnelSession(host.groupId, tunnel1.sessionId).status, "expired");
  assert.equal(store.getTunnelSession(host.groupId, tunnel2.sessionId).status, "pending");

  assert.throws(
    () => store.getPlayerSession(host.groupId, player1.playerId),
    /Player session does not exist/,
  );
  store.getPlayerSession(host.groupId, player2.playerId);
});

test("create tunnel session returns a copy, not a reference", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const election = store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  const poll = store.pollTakeover(host);
  const takeoverToken = required(poll.assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId, takeoverToken });
  store.completeTakeover({ ...host, assignmentId, takeoverToken });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const tunnel = store.createTunnelSession({ groupId: host.groupId, playerId: player.playerId });

  tunnel.status = "active";

  const fetched = store.getTunnelSession(host.groupId, tunnel.sessionId);
  assert.equal(fetched.status, "pending");
});

test("create player session returns a copy, not a reference", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });

  player.displayName = "Hacked";

  const fetched = store.getPlayerSession(host.groupId, player.playerId);
  assert.equal(fetched.displayName, "Steve");
});

type TestHost = {
  groupId: string;
  accessKey: string;
  hostId: string;
  hostToken: string;
};

function createHost(store: ReturnType<typeof createInMemoryCoordinatorStore>): TestHost {
  const group = store.createGroup({ name: "Test", ownerName: "Owner" });
  const joined = store.joinGroup({
    groupId: group.groupId,
    accessKey: group.accessKey,
    displayName: "Host",
  });
  const host = store.registerHost({
    groupId: group.groupId,
    memberId: joined.memberId,
    deviceName: "host",
    platform: "test",
    agentVersion: "0.1.0",
  });
  return { groupId: group.groupId, accessKey: group.accessKey, ...host };
}

function addHost(
  store: ReturnType<typeof createInMemoryCoordinatorStore>,
  group: TestHost,
  name: string,
): TestHost {
  const joined = store.joinGroup({
    groupId: group.groupId,
    accessKey: group.accessKey,
    displayName: name,
  });
  const host = store.registerHost({
    groupId: group.groupId,
    memberId: joined.memberId,
    deviceName: name,
    platform: "test",
    agentVersion: "0.1.0",
  });
  return { groupId: group.groupId, accessKey: group.accessKey, ...host };
}

function heartbeat(
  store: ReturnType<typeof createInMemoryCoordinatorStore>,
  host: TestHost,
  input: {
    status: "online" | "standby" | "hosting" | "unhealthy" | "offline";
    javaAvailable: boolean;
  },
): void {
  store.updateHeartbeat({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    status: input.status,
    hostScoreHints: { javaAvailable: input.javaAvailable },
  });
}

function testClock(start: string): { now: () => Date; advance: (milliseconds: number) => void } {
  let now = new Date(start).getTime();
  return {
    now: () => new Date(now),
    advance: (milliseconds) => {
      now += milliseconds;
    },
  };
}

function required<T>(value: T | null | undefined): T {
  assert.notEqual(value, null);
  assert.notEqual(value, undefined);
  return value as T;
}
