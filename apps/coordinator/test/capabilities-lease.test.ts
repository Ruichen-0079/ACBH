import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

test("capabilities and health expose alpha3 protocol metadata", async () => {
  const app = await buildApp({ logger: false });
  try {
    const health = await app.inject({ method: "GET", url: "/health" });
    assert.equal(health.statusCode, 200);
    assert.equal(health.json<{ protocolVersion: number }>().protocolVersion, 2);

    const caps = await app.inject({ method: "GET", url: "/v1/capabilities" });
    assert.equal(caps.statusCode, 200);
    const body = caps.json<{ protocolVersion: number; capabilities: string[]; authenticationMode: string }>();
    assert.equal(body.protocolVersion, 2);
    assert.equal(body.capabilities.includes("invite_management_v1"), true);
    assert.equal(body.capabilities.includes("lease_renew_v1"), true);
    assert.equal(body.authenticationMode, "host_token_or_owner_access_key");
  } finally {
    await app.close();
  }
});

test("members endpoint returns group members for authenticated host", async () => {
  const app = await buildApp({ logger: false });
  try {
    const owner = await createRegisteredHost(app, "Owner");
    const members = await app.inject({
      method: "GET",
      url: `/v1/groups/${owner.groupId}/members`,
      headers: {
        "x-acbh-host-id": owner.hostId,
        "x-acbh-host-token": owner.hostToken,
      },
    });
    assert.equal(members.statusCode, 200, members.body);
    const body = members.json<{ groupName: string; members: Array<{ role: string; isLocal: boolean }> }>();
    assert.equal(body.groupName, "Alpha3");
    assert.equal(body.members.length >= 1, true);
    assert.equal(body.members[0]?.role, "owner");
    assert.equal(body.members[0]?.isLocal, true);
  } finally {
    await app.close();
  }
});

test("whoami and ensure-active lease use authenticated host identity", async () => {
  const app = await buildApp({ logger: false });
  try {
    const owner = await createRegisteredHost(app, "Owner");

    const who = await app.inject({
      method: "GET",
      url: `/v1/groups/${owner.groupId}/whoami`,
      headers: {
        "x-acbh-host-id": owner.hostId,
        "x-acbh-host-token": owner.hostToken,
      },
    });
    assert.equal(who.statusCode, 200, who.body);
    assert.equal(who.json<{ role: string; credentialKind: string }>().role, "owner");
    assert.equal(who.json<{ credentialKind: string }>().credentialKind, "host_token");

    const ensured = await app.inject({
      method: "POST",
      url: `/v1/groups/${owner.groupId}/lease/ensure-active`,
      payload: {
        groupId: owner.groupId,
        hostId: owner.hostId,
        hostToken: owner.hostToken,
      },
    });
    assert.equal(ensured.statusCode, 200, ensured.body);
    const lease = ensured.json<{ renewed: boolean; lease: { leaseValid: boolean; currentHostIdMatches: boolean; generation: number } }>();
    assert.equal(lease.renewed, true);
    assert.equal(lease.lease.leaseValid, true);
    assert.equal(lease.lease.currentHostIdMatches, true);
    assert.equal(lease.lease.generation, 1);
  } finally {
    await app.close();
  }
});

test("invite management accepts owner host auth and rejects member host auth", async () => {
  const app = await buildApp({ logger: false });
  try {
    const owner = await createRegisteredHost(app, "Owner");
    const invite = await app.inject({
      method: "POST",
      url: `/v1/groups/${owner.groupId}/invites`,
      payload: {
        hostId: owner.hostId,
        hostToken: owner.hostToken,
        expiresInSeconds: 1800,
        oneTime: true,
      },
    });
    assert.equal(invite.statusCode, 200, invite.body);
    const inviteBody = invite.json<{ inviteCode: string }>();

    const joined = await app.inject({
      method: "POST",
      url: "/v1/invites/join",
      payload: {
        inviteCode: inviteBody.inviteCode,
        displayName: "Member",
        deviceName: "PC",
        platform: "windows",
        agentVersion: "0.4.0-alpha3",
      },
    });
    assert.equal(joined.statusCode, 200, joined.body);
    const member = joined.json<{ hostId: string; hostToken: string }>();

    const denied = await app.inject({
      method: "POST",
      url: `/v1/groups/${owner.groupId}/invites/list`,
      payload: {
        hostId: member.hostId,
        hostToken: member.hostToken,
      },
    });
    assert.equal(denied.statusCode, 403);
    assert.equal(denied.json<{ code: string }>().code, "invite_permission_denied");
  } finally {
    await app.close();
  }
});

async function createRegisteredHost(app: Awaited<ReturnType<typeof buildApp>>, ownerName: string): Promise<{
  groupId: string;
  accessKey: string;
  memberId: string;
  hostId: string;
  hostToken: string;
}> {
  const createdResponse = await app.inject({
    method: "POST",
    url: "/v1/groups",
    payload: { name: "Alpha3", ownerName },
  });
  assert.equal(createdResponse.statusCode, 200, createdResponse.body);
  const created = createdResponse.json<{ groupId: string; ownerMemberId: string; accessKey: string }>();

  const registered = await app.inject({
    method: "POST",
    url: "/v1/hosts/register",
    payload: {
      groupId: created.groupId,
      accessKey: created.accessKey,
      memberId: created.ownerMemberId,
      deviceName: "Owner-PC",
      platform: "windows",
      agentVersion: "0.4.0-alpha3",
    },
  });
  assert.equal(registered.statusCode, 200, registered.body);
  const host = registered.json<{ hostId: string; hostToken: string }>();
  return {
    groupId: created.groupId,
    accessKey: created.accessKey,
    memberId: created.ownerMemberId,
    hostId: host.hostId,
    hostToken: host.hostToken,
  };
}
