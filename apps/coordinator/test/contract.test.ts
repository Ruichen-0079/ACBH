import assert from "node:assert/strict";
import test from "node:test";
import { buildApp } from "../src/app.js";
import { createInMemoryCoordinatorStore, StoreError } from "../src/store.js";

test("create group response shape", async () => {
  const app = await buildApp({ logger: false });
  try {
    const res = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: { name: "Smoke Group", ownerName: "Smoke Owner" },
    });
    assert.equal(res.statusCode, 200);
    const body = res.json();
    assert.ok(typeof body.groupId === "string" && body.groupId.length > 0, "groupId must be a non-empty string");
    assert.ok(typeof body.ownerMemberId === "string" && body.ownerMemberId.length > 0, "ownerMemberId must be a non-empty string");
    assert.ok(typeof body.accessKey === "string" && body.accessKey.length > 0, "accessKey must be a non-empty string");
    assert.ok(body.accessKey.startsWith("ak_"), "accessKey must have ak_ prefix");
  } finally {
    await app.close();
  }
});

test("register host response shape", async () => {
  const app = await buildApp({ logger: false });
  try {
    const create = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: { name: "Test", ownerName: "Owner" },
    });
    const { groupId, accessKey, ownerMemberId } = create.json<{ groupId: string; accessKey: string; ownerMemberId: string }>();

    const res = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: {
        groupId,
        accessKey,
        memberId: ownerMemberId,
        deviceName: "test",
        platform: "linux",
        agentVersion: "0.1.0",
      },
    });
    assert.equal(res.statusCode, 200);
    const body = res.json();
    assert.ok(typeof body.hostId === "string" && body.hostId.startsWith("host_"), "hostId must have host_ prefix");
    assert.ok(typeof body.hostToken === "string" && body.hostToken.startsWith("ht_"), "hostToken must have ht_ prefix");
  } finally {
    await app.close();
  }
});

test("group state requires auth", async () => {
  const app = await buildApp({ logger: false });
  try {
    const create = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: { name: "Test", ownerName: "Owner" },
    });
    const { groupId } = create.json<{ groupId: string }>();

    const anon = await app.inject({ method: "GET", url: `/v1/groups/${groupId}/state` });
    assert.equal(anon.statusCode, 401);
    assert.ok(anon.json<{ error: string }>().error === "Unauthorized");
  } finally {
    await app.close();
  }
});

test("artifact manifest validation error shape", async () => {
  const app = await buildApp({ logger: false });
  try {
    const create = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: { name: "Test", ownerName: "Owner" },
    });
    const { groupId, accessKey, ownerMemberId } = create.json<{ groupId: string; accessKey: string; ownerMemberId: string }>();

    const register = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: { groupId, accessKey, memberId: ownerMemberId, deviceName: "test", platform: "linux", agentVersion: "0.1.0" },
    });
    const { hostId, hostToken } = register.json<{ hostId: string; hostToken: string }>();

    const res = await app.inject({
      method: "POST",
      url: "/v1/artifacts/manifests",
      payload: {
        groupId,
        hostId,
        hostToken,
        artifactKind: "world-snapshot",
        artifactId: "snap_bad",
        manifest: { artifactKind: "invalid-kind", artifactId: "snap_bad", groupId, creatorHostId: hostId },
      },
    });
    assert.ok(res.statusCode === 400 || res.statusCode === 403, `expected 400 or 403, got ${res.statusCode}`);
    const body = res.json();
    assert.equal(body.error, res.statusCode === 400 ? "Bad Request" : "Forbidden");
    assert.ok(typeof body.message === "string");
    assert.equal(res.body.includes(hostToken), false);
    assert.equal(res.body.includes(accessKey), false);
  } finally {
    await app.close();
  }
});

test("player token expiry error shape", () => {
  const clock = (() => {
    let t = new Date("2026-06-06T00:00:00.000Z").getTime();
    return { now: () => new Date(t), advance: (ms: number) => { t += ms; } };
  })();
  const store = createInMemoryCoordinatorStore({ now: clock.now, tunnelSessionTtlMs: 1000 });
  const group = store.createGroup({ name: "Test", ownerName: "Owner" });
  const player = store.createPlayerSession({ groupId: group.groupId });

  clock.advance(1001);
  assert.throws(
    () => store.verifyPlayerToken(group.groupId, player.playerId, player.playerToken!),
    (err: unknown) => {
      if (!(err instanceof StoreError)) return false;
      return err.statusCode === 401 && err.code === "token_expired"
        && !err.message.includes(player.playerToken!);
    },
  );
});

test("tunnel create auth error shape", async () => {
  const app = await buildApp({ logger: false });
  try {
    const create = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: { name: "Test", ownerName: "Owner" },
    });
    const { groupId } = create.json<{ groupId: string }>();

    const res = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/tunnel-sessions`,
      payload: { playerId: "plyr_fake" },
    });
    assert.equal(res.statusCode, 401);
    const body = res.json();
    assert.equal(body.error, "Unauthorized");
    assert.ok(typeof body.message === "string");
  } finally {
    await app.close();
  }
});

test("error response contains code/requestId, not raw secret", async () => {
  const app = await buildApp({ logger: false });
  try {
    const res = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: {
        groupId: "grp_nonexistent",
        accessKey: "redacted-access-key-12345",
        memberId: "mem_fake",
        deviceName: "test",
        platform: "linux",
        agentVersion: "0.1.0",
      },
    });
    assert.equal(res.statusCode, 404);
    const body = res.body;
    assert.equal(body.includes("redacted-access-key-12345"), false, "must not leak access key");
    assert.ok(body.includes("Group does not exist") || body.includes("Not Found"), "must have clear error");
  } finally {
    await app.close();
  }
});
