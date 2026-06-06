import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildApp } from "../src/app.js";
import {
  createInMemoryCoordinatorStore,
  type HostScoreHints,
  type LatestLocalArtifacts,
} from "../src/store.js";
import { LocalFilesystemStorage } from "../src/storage/index.js";

test("heartbeat keeps old format compatible and stores election hints", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now });
  const host = createHost(store);

  store.updateHeartbeat({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    status: "standby",
    latestLocalSnapshotId: "snap_legacy",
  });

  const stateAfterLegacy = store.getGroupState(host.groupId);
  assert.equal(stateAfterLegacy.currentHostGeneration, 0);
  assert.equal(stateAfterLegacy.hosts[0].latestLocalSnapshotId, "snap_legacy");
  assert.equal(stateAfterLegacy.hosts[0].latestLocalArtifacts["world-snapshot"], "snap_legacy");

  const latestLocalArtifacts: LatestLocalArtifacts = {
    "world-snapshot": "snap_000001",
    "server-pack": "pack_000001",
    "admin-state": "admin_000001",
  };
  const hostScoreHints: HostScoreHints = {
    cpuCores: 8,
    memoryTotalBytes: 16 * 1024 * 1024 * 1024,
    diskFreeBytes: 100 * 1024 * 1024 * 1024,
    javaAvailable: true,
  };
  store.updateHeartbeat({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    status: "online",
    latestLocalArtifacts,
    hostScoreHints,
    connection: {
      host: "100.64.0.10",
      port: 25565,
      network: "tailscale",
    },
  });

  const saved = store.getGroupState(host.groupId).hosts[0];
  assert.deepEqual(saved.latestLocalArtifacts, latestLocalArtifacts);
  assert.deepEqual(saved.hostScoreHints, hostScoreHints);
  assert.deepEqual(saved.connection, {
    host: "100.64.0.10",
    port: 25565,
    network: "tailscale",
  });
});

test("fresh standby host is eligible and stale or disallowed hosts are not", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, heartbeatTimeoutMs: 30_000 });
  const fresh = createHost(store);
  const stale = addHost(store, fresh, "stale");
  const unhealthy = addHost(store, fresh, "unhealthy");
  const offline = addHost(store, fresh, "offline");
  const hosting = addHost(store, fresh, "hosting");

  heartbeat(store, fresh, { status: "standby", javaAvailable: true });
  heartbeat(store, stale, { status: "standby", javaAvailable: true });
  clock.advance(31_000);
  heartbeat(store, fresh, { status: "standby", javaAvailable: true });
  heartbeat(store, unhealthy, { status: "unhealthy", javaAvailable: true });
  heartbeat(store, offline, { status: "offline", javaAvailable: true });
  heartbeat(store, hosting, { status: "hosting", javaAvailable: true });

  const byHost = new Map(store.evaluateCandidates(fresh.groupId).map((candidate) => [candidate.hostId, candidate]));
  assert.equal(byHost.get(fresh.hostId)?.eligible, true);
  assert.deepEqual(byHost.get(stale.hostId)?.reasons, ["stale-heartbeat"]);
  assert.deepEqual(byHost.get(unhealthy.hostId)?.reasons, ["status-unhealthy"]);
  assert.deepEqual(byHost.get(offline.hostId)?.reasons, ["status-offline"]);
  assert.deepEqual(byHost.get(hosting.hostId)?.reasons, ["status-hosting"]);
});

test("java availability and latest world snapshot control eligibility", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);
  const noJava = addHost(store, host, "no-java");

  heartbeat(store, host, { status: "standby", javaAvailable: true });
  heartbeat(store, noJava, { status: "standby", javaAvailable: false });

  let byHost = new Map(store.evaluateCandidates(host.groupId).map((candidate) => [candidate.hostId, candidate]));
  assert.equal(byHost.get(host.hostId)?.eligible, true, "no latest world allows election");
  assert.deepEqual(byHost.get(noJava.hostId)?.reasons, ["java-unavailable"]);

  recordAvailableArtifact(store, host.groupId, host.hostId, "world-snapshot", "snap_000001");
  byHost = new Map(store.evaluateCandidates(host.groupId).map((candidate) => [candidate.hostId, candidate]));
  assert.deepEqual(byHost.get(host.hostId)?.reasons, ["missing-latest-world-snapshot"]);

  heartbeat(store, host, {
    status: "standby",
    javaAvailable: true,
    latestLocalArtifacts: { "world-snapshot": "snap_000001" },
  });
  byHost = new Map(store.evaluateCandidates(host.groupId).map((candidate) => [candidate.hostId, candidate]));
  assert.equal(byHost.get(host.hostId)?.eligible, true);
  assert.ok((byHost.get(host.hostId)?.score ?? 0) > (byHost.get(noJava.hostId)?.score ?? 0));
});

test("candidate ordering is deterministic and failures reduce score", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now });
  const first = createHost(store);
  const second = addHost(store, first, "second");

  heartbeat(store, first, { status: "standby", javaAvailable: true });
  heartbeat(store, second, { status: "standby", javaAvailable: true });

  const tied = store.evaluateCandidates(first.groupId);
  const expectedFirst = [first.hostId, second.hostId].sort()[0];
  assert.equal(tied[0].hostId, expectedFirst);

  store.recordHostFailure(first.groupId, expectedFirst);
  const afterFailure = store.evaluateCandidates(first.groupId);
  assert.equal(afterFailure[0].hostId, expectedFirst === first.hostId ? second.hostId : first.hostId);
  const failed = afterFailure.find((candidate) => candidate.hostId === expectedFirst);
  const healthy = afterFailure.find((candidate) => candidate.hostId !== expectedFirst);
  assert.equal((healthy?.score ?? 0) - (failed?.score ?? 0), 10);
});

test("election selects the best eligible host and defers current host finalization", () => {
  const store = createInMemoryCoordinatorStore();
  const lower = createHost(store);
  const higher = addHost(store, lower, "higher");
  heartbeat(store, lower, { status: "standby", javaAvailable: true });
  heartbeat(store, higher, { status: "standby", javaAvailable: true });
  store.setHostManualPriority(lower.groupId, higher.hostId, 20);

  const result = store.runElection({ groupId: lower.groupId, reason: "manual" });
  assert.equal(result.ok, true);
  assert.equal(result.selectedHostId, higher.hostId);
  assert.equal(result.assignment?.hostId, higher.hostId);
  assert.equal(store.getGroupState(lower.groupId).currentHostId, null);
  assert.equal(store.getElectionStatus(lower.groupId).lastElection?.selectedHostId, higher.hostId);

  heartbeat(store, lower, { status: "unhealthy", javaAvailable: true });
  heartbeat(store, higher, { status: "offline", javaAvailable: true });
  const none = store.runElection({ groupId: lower.groupId, reason: "manual" });
  assert.equal(none.ok, false);
  assert.equal(none.selectedHostId, null);
  assert.equal(none.assignment, null);
});

test("takeover token is one-time, hashed, assigned-host-only, and completion advances generation", () => {
  const store = createInMemoryCoordinatorStore();
  const assigned = createHost(store);
  const other = addHost(store, assigned, "other");
  heartbeat(store, assigned, { status: "standby", javaAvailable: true });
  heartbeat(store, other, { status: "standby", javaAvailable: true });
  store.setHostManualPriority(assigned.groupId, assigned.hostId, 20);

  const election = store.runElection({ groupId: assigned.groupId, reason: "no-current-host" });
  const assignmentId = required(election.assignment?.assignmentId);
  assert.deepEqual(store.pollTakeover(other), { assignment: null });

  const dryRunPoll = store.pollTakeover({ ...assigned, dryRun: true });
  assert.equal(dryRunPoll.assignment?.takeoverToken, undefined);
  assert.equal(store.getTakeoverAssignment(assigned.groupId, assignmentId).takeoverTokenHash, null);

  const firstPoll = store.pollTakeover(assigned);
  const takeoverToken = required(firstPoll.assignment?.takeoverToken);
  assert.equal(firstPoll.assignment?.currentHostGeneration, 0);
  const stored = store.getTakeoverAssignment(assigned.groupId, assignmentId);
  assert.notEqual(stored.takeoverTokenHash, takeoverToken);
  assert.equal(JSON.stringify(stored).includes(takeoverToken), false);

  const secondPoll = store.pollTakeover(assigned);
  assert.equal(secondPoll.assignment?.takeoverToken, undefined);
  assert.throws(
    () =>
      store.acceptTakeover({
        ...assigned,
        assignmentId,
        takeoverToken: "wrong",
      }),
    /Invalid takeover token/,
  );

  store.acceptTakeover({ ...assigned, assignmentId, takeoverToken });
  store.completeTakeover({ ...assigned, assignmentId, takeoverToken });
  const state = store.getGroupState(assigned.groupId);
  assert.equal(state.currentHostId, assigned.hostId);
  assert.equal(state.currentHostGeneration, 1);
  assert.equal(store.getElectionStatus(assigned.groupId).activeTakeoverAssignment, null);
});

test("failed, expired, and superseded takeover assignments are handled safely", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, assignmentTtlMs: 60_000 });
  const host = createHost(store);
  heartbeat(store, host, { status: "standby", javaAvailable: true });

  const firstElection = store.runElection({ groupId: host.groupId, reason: "manual" });
  const firstId = required(firstElection.assignment?.assignmentId);
  const firstToken = required(store.pollTakeover(host).assignment?.takeoverToken);
  const secondElection = store.runElection({ groupId: host.groupId, reason: "manual" });
  assert.equal(store.getTakeoverAssignment(host.groupId, firstId).status, "cancelled");

  const secondId = required(secondElection.assignment?.assignmentId);
  const secondToken = required(store.pollTakeover(host).assignment?.takeoverToken);
  store.acceptTakeover({ ...host, assignmentId: secondId, takeoverToken: secondToken });
  store.failTakeover({
    ...host,
    assignmentId: secondId,
    takeoverToken: secondToken,
    failureReason: "pull-failed",
  });
  assert.equal(store.getGroupState(host.groupId).hosts[0].recentFailureCount, 1);

  const expiring = store.runElection({ groupId: host.groupId, reason: "manual" });
  const expiringId = required(expiring.assignment?.assignmentId);
  const expiringToken = required(store.pollTakeover(host).assignment?.takeoverToken);
  clock.advance(60_001);
  assert.throws(
    () => store.acceptTakeover({ ...host, assignmentId: expiringId, takeoverToken: expiringToken }),
    /expired/,
  );
  assert.equal(store.getTakeoverAssignment(host.groupId, expiringId).status, "expired");
  assert.notEqual(firstToken, secondToken);
});

test("timeout check skips a fresh current host and elects after it becomes stale", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, heartbeatTimeoutMs: 30_000 });
  const hostA = createHost(store);
  const hostB = addHost(store, hostA, "host-b");
  heartbeat(store, hostA, { status: "standby", javaAvailable: true });
  heartbeat(store, hostB, { status: "standby", javaAvailable: true });
  store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);

  const initial = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
  const assignmentId = required(initial.assignment?.assignmentId);
  const token = required(store.pollTakeover(hostA).assignment?.takeoverToken);
  store.acceptTakeover({ ...hostA, assignmentId, takeoverToken: token });
  store.completeTakeover({ ...hostA, assignmentId, takeoverToken: token });
  heartbeat(store, hostA, { status: "hosting", javaAvailable: true });

  assert.deepEqual(store.checkElectionTimeout(hostA.groupId), {
    electionNeeded: false,
    election: null,
  });

  clock.advance(31_000);
  heartbeat(store, hostB, { status: "standby", javaAvailable: true });
  const timedOut = store.checkElectionTimeout(hostA.groupId);
  assert.equal(timedOut.electionNeeded, true);
  assert.equal(timedOut.election?.election.reason, "heartbeat-timeout");
  assert.equal(timedOut.election?.selectedHostId, hostB.hostId);
  assert.equal(store.getGroupState(hostA.groupId).currentHostId, hostA.hostId);
});

test("election and takeover APIs require auth and never expose stored token material", async (t) => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now });
  const host = createHost(store);
  heartbeat(store, host, { status: "standby", javaAvailable: true });
  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-election-api-"));
  t.after(async () => rm(root, { force: true, recursive: true }));
  const app = await buildApp({ logger: false, store, storage: new LocalFilesystemStorage(root) });
  t.after(async () => app.close());

  const unauthorized = await app.inject({
    method: "POST",
    url: `/v1/groups/${host.groupId}/election/run`,
    payload: {
      groupId: host.groupId,
      hostId: host.hostId,
      hostToken: "wrong",
      reason: "manual",
    },
  });
  assert.equal(unauthorized.statusCode, 401);

  const run = await app.inject({
    method: "POST",
    url: `/v1/groups/${host.groupId}/election/run`,
    payload: {
      groupId: host.groupId,
      hostId: host.hostId,
      hostToken: host.hostToken,
      reason: "manual",
    },
  });
  assert.equal(run.statusCode, 200, run.body);
  assert.equal(run.json<{ selectedHostId: string }>().selectedHostId, host.hostId);

  const missingStatusAuth = await app.inject({
    method: "GET",
    url: `/v1/groups/${host.groupId}/election/status`,
  });
  assert.equal(missingStatusAuth.statusCode, 401);

  const status = await app.inject({
    method: "GET",
    url: `/v1/groups/${host.groupId}/election/status`,
    headers: hostHeaders(host),
  });
  assert.equal(status.statusCode, 200);
  assert.equal(status.body.includes("takeoverToken"), false);
  assert.equal(status.body.includes(host.hostToken), false);

  const poll = await app.inject({
    method: "POST",
    url: "/v1/hosts/takeover/poll",
    payload: {
      groupId: host.groupId,
      hostId: host.hostId,
      hostToken: host.hostToken,
    },
  });
  assert.equal(poll.statusCode, 200);
  const polled = poll.json<{ assignment: { assignmentId: string; takeoverToken: string } }>();
  assert.ok(polled.assignment.takeoverToken);
  assert.equal(JSON.stringify(store.getTakeoverAssignment(host.groupId, polled.assignment.assignmentId)).includes(polled.assignment.takeoverToken), false);
});

type TestHost = {
  groupId: string;
  accessKey: string;
  hostId: string;
  hostToken: string;
};

function createHost(store: ReturnType<typeof createInMemoryCoordinatorStore>): TestHost {
  const group = store.createGroup({ name: "Survival", ownerName: "Owner" });
  const joined = store.joinGroup({
    groupId: group.groupId,
    accessKey: group.accessKey,
    displayName: "Host A",
  });
  const host = store.registerHost({
    groupId: group.groupId,
    memberId: joined.memberId,
    deviceName: "host-a",
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
    latestLocalArtifacts?: LatestLocalArtifacts;
  },
): void {
  store.updateHeartbeat({
    groupId: host.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    status: input.status,
    latestLocalArtifacts: input.latestLocalArtifacts,
    hostScoreHints: { javaAvailable: input.javaAvailable },
  });
}

function recordAvailableArtifact(
  store: ReturnType<typeof createInMemoryCoordinatorStore>,
  groupId: string,
  creatorHostId: string,
  artifactKind: "server-pack" | "world-snapshot" | "admin-state",
  artifactId: string,
): void {
  store.recordArtifact({
    groupId,
    artifactKind,
    artifactId,
    parentArtifactId: null,
    serverPackVersion: artifactKind === "world-snapshot" ? "pack_000001" : null,
    creatorHostId,
    createdAt: "2026-06-06T00:00:00.000Z",
    status: "available",
    manifestSha256: "a".repeat(64),
    manifestObjectPath: `manifests/${artifactId}`,
    fileCount: 1,
    totalBytes: 1,
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

function hostHeaders(host: TestHost): Record<string, string> {
  return {
    "x-acbh-host-id": host.hostId,
    "x-acbh-host-token": host.hostToken,
  };
}
