import assert from "node:assert/strict";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { loadState, saveState } from "../src/persistence.js";
import {
  createInMemoryCoordinatorStore,
  InMemoryCoordinatorStore,
  type StoreSnapshot,
  type HostScoreHints,
  type LatestLocalArtifacts,
} from "../src/store.js";

function testDir() {
  return path.join(path.dirname(new URL(import.meta.url).pathname), ".tmp-persistence");
}

function stateFilePath(dir: string) {
  return path.join(dir, "coordinator-state.json");
}

async function createTempDir() {
  const dir = testDir();
  await rm(dir, { recursive: true, force: true });
  await mkdir(dir, { recursive: true });
  return dir;
}

function testClock(iso: string) {
  const date = new Date(iso);
  return { now: () => new Date(date.getTime()) };
}

function createHost(store: InMemoryCoordinatorStore) {
  const { groupId, accessKey, ownerMemberId } = store.createGroup({ name: "test", ownerName: "alice" });
  const { hostId, hostToken } = store.registerHost({
    groupId,
    memberId: ownerMemberId,
    deviceName: "test-device",
    platform: "linux",
    agentVersion: "1.0.0",
  });
  return { groupId, hostId, hostToken };
}

async function saveAndReload(dir: string, store: InMemoryCoordinatorStore): Promise<InMemoryCoordinatorStore> {
  const file = stateFilePath(dir);
  await saveState(file, store.snapshot());
  const snapshot = await loadState(file);
  assert.ok(snapshot !== null, "snapshot should not be null after save");
  return InMemoryCoordinatorStore.fromSnapshot(snapshot!);
}

test("starts empty when state file does not exist", async () => {
  const dir = await createTempDir();
  const file = stateFilePath(dir);
  const snapshot = await loadState(file);
  assert.equal(snapshot, null);
  await rm(dir, { recursive: true });
});

test("saves state after group and host registration", async () => {
  const dir = await createTempDir();
  const store = createInMemoryCoordinatorStore();
  createHost(store);

  const file = stateFilePath(dir);
  await saveState(file, store.snapshot());

  const raw = await readFile(file, "utf8");
  const parsed = JSON.parse(raw);
  assert.equal(parsed.version, 1);
  assert.equal(parsed.state.groups.length, 1);
  assert.equal(parsed.state.groups[0].hosts.length, 1);
  assert.equal(parsed.state.groups[0].hosts[0].hostId.includes("host_"), true);
  assert.equal(parsed.state.groups[0].hosts[0].hostTokenHash.length, 64);
  await rm(dir, { recursive: true });
});

test("reloads groups and hosts after restart", async () => {
  const dir = await createTempDir();
  const store1 = createInMemoryCoordinatorStore();
  const { groupId } = createHost(store1);

  const store2 = await saveAndReload(dir, store1);
  const state = store2.getGroupState(groupId);
  assert.equal(state.groupId, groupId);
  assert.equal(state.hosts.length, 1);
  assert.equal(state.hosts[0].deviceName, "test-device");
  await rm(dir, { recursive: true });
});

test("reloads artifact records and latest pointers", async () => {
  const dir = await createTempDir();
  const clock = testClock("2026-06-06T00:10:00.000Z");
  const store1 = createInMemoryCoordinatorStore({ now: clock.now });
  const { groupId, hostId, hostToken } = createHost(store1);

  store1.recordArtifactFromHost({
    metadata: {
      groupId,
      artifactKind: "world-snapshot",
      artifactId: "snap_1",
      parentArtifactId: null,
      serverPackVersion: "1.21",
      creatorHostId: hostId,
      createdAt: "2026-06-06T00:11:00.000Z",
      status: "available",
      manifestSha256: "a".repeat(64),
      manifestObjectPath: "manifests/snap_1.json",
      fileCount: 10,
      totalBytes: 1000,
    },
    hostId,
    hostToken,
  });

  const store2 = await saveAndReload(dir, store1);
  const artifacts = store2.listArtifacts(groupId);
  assert.equal(artifacts.length, 1);
  assert.equal(artifacts[0].artifactId, "snap_1");

  const latest = store2.getLatestArtifact(groupId, "world-snapshot");
  assert.equal(latest.artifactId, "snap_1");
  await rm(dir, { recursive: true });
});

test("reloads currentHostId and currentHostGeneration after takeover complete", async () => {
  const dir = await createTempDir();
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store1 = createInMemoryCoordinatorStore({ now: clock.now });
  const { groupId, hostId, hostToken } = createHost(store1);

  store1.updateHeartbeat({
    groupId,
    hostId,
    hostToken,
    status: "online",
    latestLocalArtifacts: { "world-snapshot": "snap_1" },
  });

  const election = store1.runElection({ groupId, reason: "manual" });
  assert.ok(election.assignment !== null, "expected an assignment");

  const poll = store1.pollTakeover({ groupId, hostId, hostToken });
  assert.ok(poll.assignment?.takeoverToken, "expected a takeover token");

  store1.acceptTakeover({
    groupId,
    hostId,
    hostToken,
    assignmentId: poll.assignment!.assignmentId,
    takeoverToken: poll.assignment!.takeoverToken!,
  });

  store1.completeTakeover({
    groupId,
    hostId,
    hostToken,
    assignmentId: poll.assignment!.assignmentId,
    takeoverToken: poll.assignment!.takeoverToken!,
  });

  const status1 = store1.getElectionStatus(groupId);
  assert.equal(status1.currentHostId, hostId);
  assert.equal(status1.currentHostGeneration, 1);

  const store2 = await saveAndReload(dir, store1);
  const status2 = store2.getElectionStatus(groupId);
  assert.equal(status2.currentHostId, hostId);
  assert.equal(status2.currentHostGeneration, 1);
  await rm(dir, { recursive: true });
});

test("reloads active assignment without raw takeover token", async () => {
  const dir = await createTempDir();
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store1 = createInMemoryCoordinatorStore({ now: clock.now });
  const { groupId, hostId, hostToken } = createHost(store1);

  store1.updateHeartbeat({
    groupId,
    hostId,
    hostToken,
    status: "online",
  });

  store1.runElection({ groupId, reason: "manual" });
  store1.pollTakeover({ groupId, hostId, hostToken });

  const file = stateFilePath(dir);
  await saveState(file, store1.snapshot());

  const raw = await readFile(file, "utf8");
  const parsed = JSON.parse(raw);
  assert.equal(parsed.state.groups.length, 1);
  const group = parsed.state.groups[0];
  assert.ok(group.takeoverAssignments.length >= 1);
  const assignment = group.takeoverAssignments[0];
  assert.equal(typeof assignment.takeoverTokenHash, "string");
  assert.ok(assignment.takeoverTokenHash.length > 0, "takeoverTokenHash should be set");
  assert.equal(assignment.takeoverTokenHash, assignment.takeoverTokenHash.toLowerCase());
  await rm(dir, { recursive: true });
});

test("corrupt JSON fails with clear error", async () => {
  const dir = await createTempDir();
  const file = stateFilePath(dir);
  await mkdir(path.dirname(file), { recursive: true });
  await writeFile(file, "this is not JSON", "utf8");

  try {
    await loadState(file);
    assert.fail("expected error for corrupt JSON");
  } catch (err) {
    assert.ok(err instanceof Error);
    assert.ok(err.message.includes("corrupt"));
  }
  await rm(dir, { recursive: true });
});

test("unknown version fails with clear error", async () => {
  const dir = await createTempDir();
  const file = stateFilePath(dir);
  await mkdir(path.dirname(file), { recursive: true });
  await writeFile(file, JSON.stringify({ version: 99, savedAt: new Date().toISOString(), state: { groups: [] } }), "utf8");

  try {
    await loadState(file);
    assert.fail("expected error for unknown version");
  } catch (err) {
    assert.ok(err instanceof Error);
    assert.ok(err.message.includes("Unknown"), `expected 'Unknown' in message, got: ${err.message}`);
    assert.ok(err.message.includes("99"), `expected '99' in message, got: ${err.message}`);
  }
  await rm(dir, { recursive: true });
});

test("save uses temp file and rename", async () => {
  const dir = await createTempDir();
  const file = stateFilePath(dir);
  const store = createInMemoryCoordinatorStore();
  createHost(store);

  await saveState(file, store.snapshot());

  const entries = await import("node:fs/promises").then((fs) => fs.readdir(path.dirname(file)));
  assert.ok(!entries.some((e: string) => e.endsWith(".tmp")), "no .tmp file should remain after successful save");

  const raw = await readFile(file, "utf8");
  const parsed = JSON.parse(raw);
  assert.equal(parsed.state.groups[0].hosts[0].hostTokenHash.length, 64);
  await rm(dir, { recursive: true });
});

test("GC after reload still protects latest artifacts", async () => {
  const dir = await createTempDir();
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store1 = createInMemoryCoordinatorStore({ now: clock.now, gcMinAgeMs: 0 });
  const { groupId, hostId, hostToken } = createHost(store1);

  store1.recordArtifactFromHost({
    metadata: {
      groupId,
      artifactKind: "world-snapshot",
      artifactId: "old_snap",
      parentArtifactId: null,
      serverPackVersion: "1.21",
      creatorHostId: hostId,
      createdAt: "2026-06-05T00:00:00.000Z",
      status: "available",
      manifestSha256: "a".repeat(64),
      manifestObjectPath: "manifests/old.json",
      fileCount: 5,
      totalBytes: 500,
    },
    hostId,
    hostToken,
  });

  store1.recordArtifactFromHost({
    metadata: {
      groupId,
      artifactKind: "world-snapshot",
      artifactId: "latest_snap",
      parentArtifactId: null,
      serverPackVersion: "1.21",
      creatorHostId: hostId,
      createdAt: "2026-06-06T00:11:00.000Z",
      status: "available",
      manifestSha256: "b".repeat(64),
      manifestObjectPath: "manifests/latest.json",
      fileCount: 10,
      totalBytes: 1000,
    },
    hostId,
    hostToken,
  });

  const store2 = await saveAndReload(dir, store1);
  assert.ok(store2.getLatestArtifact(groupId, "world-snapshot").artifactId, "latest_snap");
  await rm(dir, { recursive: true });
});
