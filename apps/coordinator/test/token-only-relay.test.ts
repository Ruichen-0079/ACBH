import assert from "node:assert/strict";
import test from "node:test";
import { buildApp } from "../src/app.js";

const TEST_ACCESS_TOKEN = "acbh-test-access-token";

async function withAccessToken<T>(fn: () => Promise<T> | T): Promise<T> {
  const previous = process.env.ACBH_ACCESS_TOKEN;
  process.env.ACBH_ACCESS_TOKEN = TEST_ACCESS_TOKEN;
  try {
    return await fn();
  } finally {
    if (previous === undefined) {
      delete process.env.ACBH_ACCESS_TOKEN;
    } else {
      process.env.ACBH_ACCESS_TOKEN = previous;
    }
  }
}

function bearerHeaders(token: string = TEST_ACCESS_TOKEN): Record<string, string> {
  return { authorization: `Bearer ${token}` };
}

function tokenOnlyHostHeaders(
  instanceId: string,
  deviceId: string,
  token: string = TEST_ACCESS_TOKEN,
): Record<string, string> {
  return {
    "x-acbh-host-id": deviceId,
    "x-acbh-host-token": token,
    "authorization": `Bearer ${token}`,
  };
}

test("bootstrap upserts instance without pre-existing group", async () => {
  await withAccessToken(async () => {
    const app = await buildApp({ logger: false });
    try {
      const response = await app.inject({
        method: "POST",
        url: "/v1/bootstrap",
        headers: bearerHeaders(),
        payload: {
          instanceId: "acbh_instance_test",
          instanceName: "私人实例",
          deviceId: "acbh_device_test",
          deviceName: "测试设备",
          serverId: "srv_test",
          serverName: "测试服务端",
        },
      });
      assert.equal(response.statusCode, 200, response.body);
      const body = response.json<{ ok: boolean; groupId: string; hostId: string; upserted: boolean }>();
      assert.equal(body.ok, true);
      assert.equal(body.groupId, "acbh_instance_test");
      assert.equal(body.hostId, "acbh_device_test");
      assert.equal(body.upserted, true);

      const repeat = await app.inject({
        method: "POST",
        url: "/v1/bootstrap",
        headers: bearerHeaders(),
        payload: {
          instanceId: "acbh_instance_test",
          instanceName: "私人实例",
          deviceId: "acbh_device_test",
          deviceName: "测试设备",
          serverId: "srv_test",
          serverName: "测试服务端",
        },
      });
      assert.equal(repeat.statusCode, 200, repeat.body);
      assert.equal(repeat.json<{ upserted: boolean }>().upserted, true);
    } finally {
      await app.close();
    }
  });
});

test("invalid access token returns 401", async () => {
  await withAccessToken(async () => {
    const app = await buildApp({ logger: false });
    try {
      const response = await app.inject({
        method: "POST",
        url: "/v1/bootstrap",
        headers: bearerHeaders("wrong-token"),
        payload: {
          instanceId: "acbh_instance_test",
          instanceName: "私人实例",
          deviceId: "acbh_device_test",
          deviceName: "测试设备",
          serverId: "srv_test",
          serverName: "测试服务端",
        },
      });
      assert.equal(response.statusCode, 401, response.body);
      assert.match(response.body, /access_token_invalid|Invalid access token/);
    } finally {
      await app.close();
    }
  });
});

test("legacy whoami and ensure-active auto upsert in token-only mode", async () => {
  await withAccessToken(async () => {
    const app = await buildApp({ logger: false });
    try {
      const whoami = await app.inject({
        method: "GET",
        url: "/v1/groups/acbh_instance_legacy/whoami",
        headers: tokenOnlyHostHeaders("acbh_instance_legacy", "acbh_device_legacy"),
      });
      assert.equal(whoami.statusCode, 200, whoami.body);
      assert.equal(whoami.json<{ ok: boolean; credentialKind: string }>().credentialKind, "access_token");

      const ensured = await app.inject({
        method: "POST",
        url: "/v1/groups/acbh_instance_legacy/lease/ensure-active",
        payload: {
          groupId: "acbh_instance_legacy",
          hostId: "acbh_device_legacy",
          hostToken: TEST_ACCESS_TOKEN,
        },
      });
      assert.equal(ensured.statusCode, 200, ensured.body);
      const lease = ensured.json<{ ok: boolean; lease: { currentHostIdMatches: boolean } }>();
      assert.equal(lease.ok, true);
      assert.equal(lease.lease.currentHostIdMatches, true);
      assert.equal(ensured.body.includes("Group does not exist"), false);
    } finally {
      await app.close();
    }
  });
});

test("capabilities expose token-only relay capabilities", async () => {
  await withAccessToken(async () => {
    const app = await buildApp({ logger: false });
    try {
      const caps = await app.inject({ method: "GET", url: "/v1/capabilities" });
      assert.equal(caps.statusCode, 200, caps.body);
      const body = caps.json<{ capabilities: string[]; authenticationMode: string }>();
      assert.equal(body.capabilities.includes("token_only_relay_v1"), true);
      assert.equal(body.capabilities.includes("bootstrap_upsert_v1"), true);
      assert.equal(body.authenticationMode, "access_token_bearer");
    } finally {
      await app.close();
    }
  });
});