import assert from "node:assert/strict";
import test from "node:test";
import { buildApp } from "../src/app.js";
import { createInMemoryCoordinatorStore, StoreError } from "../src/store.js";

type TestHost = {
  groupId: string;
  accessKey: string;
  hostId: string;
  hostToken: string;
};

function createHost(store: ReturnType<typeof createInMemoryCoordinatorStore>): TestHost {
  const group = store.createGroup({ name: "AuthTest", ownerName: "Owner" });
  const joined = store.joinGroup({
    groupId: group.groupId,
    accessKey: group.accessKey,
    displayName: "Member",
  });
  const host = store.registerHost({
    groupId: group.groupId,
    accessKey: group.accessKey,
    memberId: joined.memberId,
    deviceName: "test-device",
    platform: "test",
    agentVersion: "0.1.0",
  });
  return { groupId: group.groupId, accessKey: group.accessKey, ...host };
}

function hostHeaders(host: TestHost): Record<string, string> {
  return { "x-acbh-host-id": host.hostId, "x-acbh-host-token": host.hostToken };
}

test("host token cannot access wrong group's state", () => {
  const store = createInMemoryCoordinatorStore();
  const hostA = createHost(store);
  const hostB = createHost(store);

  assert.throws(
    () => store.verifyHost({ groupId: hostB.groupId, hostId: hostA.hostId, hostToken: hostA.hostToken }),
    /Host does not exist in group/,
  );

  assert.throws(
    () => store.verifyHost({ groupId: "grp_nonexistent", hostId: hostA.hostId, hostToken: hostA.hostToken }),
    /Group does not exist/,
  );
});

test("player token cannot call host API endpoints", async () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);
  store.updateHeartbeat({
    groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken, status: "online",
    hostScoreHints: { javaAvailable: true },
  });
  store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const poll = store.pollTakeover(host);
  const token = poll.assignment?.takeoverToken;
  assert.ok(token);
  store.acceptTakeover({ ...host, assignmentId: poll.assignment!.assignmentId, takeoverToken: token });
  store.completeTakeover({ ...host, assignmentId: poll.assignment!.assignmentId, takeoverToken: token });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const app = await buildApp({ logger: false, store });

  try {
    const hostEndpoint = await app.inject({
      method: "GET",
      url: `/v1/groups/${host.groupId}/artifacts/latest?artifactKind=world-snapshot`,
      headers: { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! },
    });
    assert.equal(hostEndpoint.statusCode, 401);

    const electionEndpoint = await app.inject({
      method: "GET",
      url: `/v1/groups/${host.groupId}/election/status`,
      headers: { "x-acbh-player-id": player.playerId, "x-acbh-player-token": player.playerToken! },
    });
    assert.equal(electionEndpoint.statusCode, 401);
  } finally {
    await app.close();
  }
});

test("host token cannot call player tunnel API", async () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);
  store.updateHeartbeat({
    groupId: host.groupId, hostId: host.hostId, hostToken: host.hostToken, status: "online",
    hostScoreHints: { javaAvailable: true },
  });
  store.runElection({ groupId: host.groupId, reason: "no-current-host" });
  const poll = store.pollTakeover(host);
  store.acceptTakeover({ ...host, assignmentId: poll.assignment!.assignmentId, takeoverToken: poll.assignment!.takeoverToken! });
  store.completeTakeover({ ...host, assignmentId: poll.assignment!.assignmentId, takeoverToken: poll.assignment!.takeoverToken! });

  const player = store.createPlayerSession({ groupId: host.groupId, displayName: "Steve" });
  const app = await buildApp({ logger: false, store });

  try {
    const tunnelResponse = await app.inject({
      method: "POST",
      url: `/v1/groups/${host.groupId}/tunnel-sessions`,
      headers: hostHeaders(host),
      payload: { playerId: player.playerId },
    });
    assert.equal(tunnelResponse.statusCode, 401);
  } finally {
    await app.close();
  }
});

test("revoked access key is rejected", () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);

  const result = store.verifyGroupAccessKey(host.groupId, host.accessKey);
  assert.equal(result, undefined);

  assert.throws(
    () => store.verifyGroupAccessKey(host.groupId, "ak_wrong_revoked"),
    /Invalid access key/,
  );

  assert.throws(
    () => store.joinGroup({ groupId: host.groupId, accessKey: "ak_wrong", displayName: "Attacker" }),
    /Invalid access key/,
  );
});

test("invite lifecycle uses hashes, one-time use, revoke, and no shared access key", () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "InviteTest", ownerName: "Owner" });
  const invite = store.createInvite({ groupId: group.groupId, accessKey: group.accessKey, expiresInSeconds: 1800, oneTime: true });
  assert.match(invite.inviteCode, /^ACBH-[A-F0-9]{6}-[A-F0-9]{6}$/);

  const snapshot = store.snapshot();
  const storedInvite = snapshot.groups[0].invites?.[0];
  assert.ok(storedInvite);
  assert.equal("inviteCode" in storedInvite, false);
  assert.notEqual(storedInvite.inviteCodeHash, invite.inviteCode);

  const joined = store.joinWithInvite({
    inviteCode: invite.inviteCode,
    displayName: "Member",
    deviceName: "MSI",
    platform: "windows",
    agentVersion: "0.1.0",
  });
  assert.equal(joined.groupId, group.groupId);
  assert.ok(joined.hostToken);
  assert.equal("accessKey" in joined, false);
  assert.throws(
    () => store.joinWithInvite({ inviteCode: invite.inviteCode, displayName: "Again", deviceName: "PC", platform: "windows", agentVersion: "0.1.0" }),
    /Invalid or expired invite code/,
  );

  const second = store.createInvite({ groupId: group.groupId, accessKey: group.accessKey, expiresInSeconds: 1800, oneTime: true });
  const listed = store.listInvites({ groupId: group.groupId, accessKey: group.accessKey });
  assert.equal(listed.invites.some((i) => i.inviteId === second.inviteId), true);
  assert.equal(JSON.stringify(listed).includes(second.inviteCode), false);
  store.revokeInvite({ groupId: group.groupId, accessKey: group.accessKey, inviteId: second.inviteId });
  assert.throws(
    () => store.joinWithInvite({ inviteCode: second.inviteCode, displayName: "Revoked", deviceName: "PC", platform: "windows", agentVersion: "0.1.0" }),
    /Invalid or expired invite code/,
  );
});

test("ordinary invite member cannot generate or revoke invites", () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "InviteTest", ownerName: "Owner" });
  const invite = store.createInvite({ groupId: group.groupId, accessKey: group.accessKey, expiresInSeconds: 1800, oneTime: true });
  const joined = store.joinWithInvite({ inviteCode: invite.inviteCode, displayName: "Member", deviceName: "MSI", platform: "windows", agentVersion: "0.1.0" });
  assert.ok(joined.hostToken);
  assert.throws(() => store.createInvite({ groupId: group.groupId, accessKey: joined.hostToken, expiresInSeconds: 1800, oneTime: true }), /Invalid access key/);
  assert.throws(() => store.revokeInvite({ groupId: group.groupId, accessKey: joined.hostToken, inviteId: invite.inviteId }), /Invalid access key/);
});

test("expired player token rejected in verifyPlayerToken", () => {
  const clock = testClock("2026-06-06T00:00:00.000Z");
  const store = createInMemoryCoordinatorStore({ now: clock.now, tunnelSessionTtlMs: 5_000 });
  const host = createHost(store);

  const player = store.createPlayerSession({ groupId: host.groupId });
  store.verifyPlayerToken(host.groupId, player.playerId, player.playerToken!);

  clock.advance(5_001);

  assert.throws(
    () => store.verifyPlayerToken(host.groupId, player.playerId, player.playerToken!),
    (err: unknown) =>
      err instanceof StoreError && err.statusCode === 401 && err.code === "token_expired",
  );
});

test("group state through host auth never exposes hostToken or accessKey", async () => {
  const store = createInMemoryCoordinatorStore();
  const host = createHost(store);
  const app = await buildApp({ logger: false, store });

  try {
    const hostState = await app.inject({
      method: "GET",
      url: `/v1/groups/${host.groupId}/state`,
      headers: hostHeaders(host),
    });
    assert.equal(hostState.statusCode, 200);
    const body = hostState.body;
    assert.equal(body.includes(host.hostToken), false);
    assert.equal(body.includes(host.accessKey), false);

    const accessKeyState = await app.inject({
      method: "GET",
      url: `/v1/groups/${host.groupId}/state`,
      headers: { "x-acbh-access-key": host.accessKey },
    });
    assert.equal(accessKeyState.statusCode, 200);
    assert.equal(accessKeyState.body.includes(host.accessKey), false);
    assert.equal(accessKeyState.body.includes(host.hostToken), false);
  } finally {
    await app.close();
  }
});

function testClock(start: string): { now: () => Date; advance: (ms: number) => void } {
  let t = new Date(start).getTime();
  return {
    now: () => new Date(t),
    advance: (ms: number) => { t += ms; },
  };
}
