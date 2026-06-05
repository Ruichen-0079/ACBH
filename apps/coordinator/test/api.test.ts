import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

test("in-memory group join, host registration, heartbeat, and debug state", async () => {
  const app = await buildApp({ logger: false });

  try {
    const createResponse = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: {
        name: "Survival Server",
        ownerName: "Owner",
      },
    });
    assert.equal(createResponse.statusCode, 200);
    const created = createResponse.json<{
      groupId: string;
      ownerMemberId: string;
      accessKey: string;
    }>();

    const deniedJoin = await app.inject({
      method: "POST",
      url: `/v1/groups/${created.groupId}/join`,
      payload: {
        accessKey: "wrong",
        displayName: "PlayerA",
      },
    });
    assert.equal(deniedJoin.statusCode, 401);

    const joinResponse = await app.inject({
      method: "POST",
      url: `/v1/groups/${created.groupId}/join`,
      payload: {
        accessKey: created.accessKey,
        displayName: "PlayerA",
      },
    });
    assert.equal(joinResponse.statusCode, 200);
    const joined = joinResponse.json<{ memberId: string; role: string }>();
    assert.equal(joined.role, "member");

    const registerResponse = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: {
        groupId: created.groupId,
        memberId: joined.memberId,
        deviceName: "PlayerA-PC",
        platform: "windows",
        agentVersion: "0.1.0",
      },
    });
    assert.equal(registerResponse.statusCode, 200);
    const registered = registerResponse.json<{ hostId: string; hostToken: string }>();

    const heartbeatResponse = await app.inject({
      method: "POST",
      url: "/v1/hosts/heartbeat",
      payload: {
        groupId: created.groupId,
        hostId: registered.hostId,
        hostToken: registered.hostToken,
        status: "standby",
        latestLocalSnapshotId: null,
      },
    });
    assert.equal(heartbeatResponse.statusCode, 200);
    assert.deepEqual(heartbeatResponse.json(), {
      ok: true,
      hostId: registered.hostId,
      status: "standby",
    });

    const stateResponse = await app.inject({
      method: "GET",
      url: `/v1/groups/${created.groupId}/state`,
    });
    assert.equal(stateResponse.statusCode, 200);
    const stateText = stateResponse.body;
    const state = stateResponse.json<{
      groupId: string;
      currentHostId: string | null;
      latestSnapshotId: string | null;
      members: unknown[];
      hosts: unknown[];
    }>();
    assert.equal(state.groupId, created.groupId);
    assert.equal(state.currentHostId, null);
    assert.equal(state.latestSnapshotId, null);
    assert.equal(state.members.length, 2);
    assert.equal(state.hosts.length, 1);
    assert.equal(stateText.includes(created.accessKey), false);
    assert.equal(stateText.includes(registered.hostToken), false);
  } finally {
    await app.close();
  }
});
