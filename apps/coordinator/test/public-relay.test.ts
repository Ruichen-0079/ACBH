import assert from "node:assert/strict";
import test from "node:test";

import { selectPublicRelayGroup } from "../src/public-relay.js";
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
