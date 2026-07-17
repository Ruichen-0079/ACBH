import assert from "node:assert/strict";
import test from "node:test";
import { createInMemoryCoordinatorStore, InMemoryCoordinatorStore } from "../src/store.js";

test("expired invite is rejected", () => {
  const clock = testClock("2026-06-01T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now });
  const group = store.createGroup({ name: "ExpireTest", ownerName: "Owner" });
  const invite = store.createInvite({
    groupId: group.groupId,
    accessKey: group.accessKey,
    expiresInSeconds: 60,
    oneTime: true,
  });
  clock.advance(61_000);
  assert.throws(
    () =>
      store.joinWithInvite({
        inviteCode: invite.inviteCode,
        displayName: "Late",
        deviceName: "PC",
        platform: "windows",
        agentVersion: "0.4.0-alpha2",
      }),
    /Invalid or expired invite code/,
  );
});

test("reusable invite allows multiple joins until revoked", () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "ReuseTest", ownerName: "Owner" });
  const invite = store.createInvite({
    groupId: group.groupId,
    accessKey: group.accessKey,
    expiresInSeconds: 3600,
    oneTime: false,
  });
  const first = store.joinWithInvite({
    inviteCode: invite.inviteCode,
    displayName: "A",
    deviceName: "PC1",
    platform: "windows",
    agentVersion: "0.4.0-alpha2",
  });
  const second = store.joinWithInvite({
    inviteCode: invite.inviteCode,
    displayName: "B",
    deviceName: "PC2",
    platform: "windows",
    agentVersion: "0.4.0-alpha2",
  });
  assert.notEqual(first.hostId, second.hostId);
});

test("one-time invite only allows a single successful join", () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "OneTime", ownerName: "Owner" });
  const invite = store.createInvite({
    groupId: group.groupId,
    accessKey: group.accessKey,
    expiresInSeconds: 3600,
    oneTime: true,
  });
  store.joinWithInvite({
    inviteCode: invite.inviteCode,
    displayName: "First",
    deviceName: "PC",
    platform: "windows",
    agentVersion: "0.4.0-alpha2",
  });
  assert.throws(
    () =>
      store.joinWithInvite({
        inviteCode: invite.inviteCode,
        displayName: "Second",
        deviceName: "PC2",
        platform: "windows",
        agentVersion: "0.4.0-alpha2",
      }),
    /Invalid or expired invite code/,
  );
});

test("invite cannot be used across groups", () => {
  const store = createInMemoryCoordinatorStore();
  const groupA = store.createGroup({ name: "A", ownerName: "OwnerA" });
  const groupB = store.createGroup({ name: "B", ownerName: "OwnerB" });
  const invite = store.createInvite({
    groupId: groupA.groupId,
    accessKey: groupA.accessKey,
    expiresInSeconds: 3600,
    oneTime: true,
  });
  const joined = store.joinWithInvite({
    inviteCode: invite.inviteCode,
    displayName: "Member",
    deviceName: "PC",
    platform: "windows",
    agentVersion: "0.4.0-alpha2",
  });
  assert.equal(joined.groupId, groupA.groupId);
  assert.notEqual(joined.groupId, groupB.groupId);
});

test("invite state survives persistence reload", () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "Persist", ownerName: "Owner" });
  const invite = store.createInvite({
    groupId: group.groupId,
    accessKey: group.accessKey,
    expiresInSeconds: 3600,
    oneTime: true,
  });
  store.joinWithInvite({
    inviteCode: invite.inviteCode,
    displayName: "Member",
    deviceName: "PC",
    platform: "windows",
    agentVersion: "0.4.0-alpha2",
  });
  const snapshot = store.snapshot();
  const reloaded = InMemoryCoordinatorStore.fromSnapshot(snapshot);
  assert.throws(
    () =>
      reloaded.joinWithInvite({
        inviteCode: invite.inviteCode,
        displayName: "Again",
        deviceName: "PC2",
        platform: "windows",
        agentVersion: "0.4.0-alpha2",
      }),
    /Invalid or expired invite code/,
  );
});

test("invalid invite join error does not echo submitted code", () => {
  const store = createInMemoryCoordinatorStore();
  const secretCode = "ACBH-DEADBE-EF0123";
  try {
    store.joinWithInvite({
      inviteCode: secretCode,
      displayName: "X",
      deviceName: "PC",
      platform: "windows",
      agentVersion: "0.4.0-alpha2",
    });
    assert.fail("expected join failure");
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    assert.equal(message.includes(secretCode), false);
  }
});

test("invite codes are unpredictable and not derived from access key", () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "Random", ownerName: "Owner" });
  const a = store.createInvite({ groupId: group.groupId, accessKey: group.accessKey, expiresInSeconds: 600, oneTime: true });
  const b = store.createInvite({ groupId: group.groupId, accessKey: group.accessKey, expiresInSeconds: 600, oneTime: true });
  assert.notEqual(a.inviteCode, b.inviteCode);
  assert.notEqual(a.inviteCode, group.accessKey);
  assert.match(a.inviteCode, /^ACBH-[0-9A-F]{6}-[0-9A-F]{6}$/);
});

function testClock(start: string): { now: () => Date; advance: (ms: number) => void } {
  let t = new Date(start).getTime();
  return {
    now: () => new Date(t),
    advance: (ms: number) => {
      t += ms;
    },
  };
}