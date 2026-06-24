import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test, type TestContext } from "node:test";
import { buildApp } from "../src/app.js";
import { LocalFilesystemStorage, type WorldSnapshotManifest } from "../src/storage/index.js";

test("world backup plan, object upload, commit, latest, pin, and delete guards", async (t) => {
  const app = await buildTestApp(t);
  try {
    const { groupId, hostId, hostToken, generation } = await createCurrentHost(app);
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const manifest = sampleWorldManifest({
      groupId,
      hostId,
      generation,
      sha256: objectSha,
      size: content.byteLength,
      snapshotId: "ws_000001",
    });

    const plan = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/plan`,
      payload: {
        hostId,
        hostToken,
        hostGeneration: generation,
        objects: [{ sha256: objectSha, size: content.byteLength, path: "world/region/r.0.0.mca" }],
      },
    });
    assert.equal(plan.statusCode, 200, plan.body);
    assert.equal(plan.json<{ missingObjects: unknown[] }>().missingObjects.length, 1);

    const badObject = await app.inject({
      method: "PUT",
      url: `/v1/groups/${groupId}/world-objects/${sha256(Buffer.from("wrong"))}`,
      headers: {
        "content-type": "application/octet-stream",
        ...hostHeaders(hostId, hostToken),
      },
      payload: content,
    });
    assert.equal(badObject.statusCode, 400);

    const uploadObject = await app.inject({
      method: "PUT",
      url: `/v1/groups/${groupId}/world-objects/${objectSha}`,
      headers: {
        "content-type": "application/octet-stream",
        ...hostHeaders(hostId, hostToken),
      },
      payload: content,
    });
    assert.equal(uploadObject.statusCode, 200, uploadObject.body);
    assert.equal(uploadObject.json<{ exists: boolean }>().exists, false);

    const duplicatePlan = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/plan`,
      payload: {
        hostId,
        hostToken,
        hostGeneration: generation,
        objects: [{ sha256: objectSha, size: content.byteLength }],
      },
    });
    assert.equal(duplicatePlan.statusCode, 200, duplicatePlan.body);
    assert.equal(duplicatePlan.json<{ missingObjects: unknown[] }>().missingObjects.length, 0);

    const commit = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/commit`,
      payload: {
        hostId,
        hostToken,
        hostGeneration: generation,
        manifest,
      },
    });
    assert.equal(commit.statusCode, 200, commit.body);
    assert.equal(commit.json<{ snapshotId: string }>().snapshotId, "ws_000001");

    const latest = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/world-backups/latest?consistentOnly=true`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(latest.statusCode, 200, latest.body);
    assert.equal(latest.json<{ metadata: { snapshotId: string; consistent: boolean } }>().metadata.snapshotId, "ws_000001");
    assert.equal(latest.json<{ manifest: WorldSnapshotManifest }>().manifest.files[0].sha256, objectSha);

    const download = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/world-objects/${objectSha}`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(download.statusCode, 200, download.body);
    assert.deepEqual(download.rawPayload, content);

    const pin = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/ws_000001/pin`,
      payload: { hostId, hostToken, pinned: true },
    });
    assert.equal(pin.statusCode, 200, pin.body);

    const deleteLatest = await app.inject({
      method: "DELETE",
      url: `/v1/groups/${groupId}/world-backups/ws_000001`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(deleteLatest.statusCode, 409);
  } finally {
    await app.close();
  }
});

test("world backup commit rejects non-current host and missing objects", async (t) => {
  const app = await buildTestApp(t);
  try {
    const { groupId, hostId, hostToken, generation, accessKey } = await createCurrentHost(app);
    const otherHost = await registerHost(app, groupId, accessKey, "Other");
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const manifest = sampleWorldManifest({
      groupId,
      hostId,
      generation,
      sha256: objectSha,
      size: content.byteLength,
      snapshotId: "ws_000001",
    });

    const notCurrent = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/plan`,
      payload: {
        hostId: otherHost.hostId,
        hostToken: otherHost.hostToken,
        hostGeneration: generation,
        objects: [{ sha256: objectSha, size: content.byteLength }],
      },
    });
    assert.equal(notCurrent.statusCode, 403, notCurrent.body);
    assert.equal(notCurrent.json<{ code: string }>().code, "not_current_host");

    const missingCommit = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/commit`,
      payload: {
        hostId,
        hostToken,
        hostGeneration: generation,
        manifest,
      },
    });
    assert.equal(missingCommit.statusCode, 400, missingCommit.body);
    assert.equal(missingCommit.json<{ code: string }>().code, "missing_world_object");
  } finally {
    await app.close();
  }
});

test("world backup second commit reuses objects and updates latest atomically", async (t) => {
  const app = await buildTestApp(t);
  try {
    const { groupId, hostId, hostToken, generation } = await createCurrentHost(app);
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const first = sampleWorldManifest({
      groupId,
      hostId,
      generation,
      sha256: objectSha,
      size: content.byteLength,
      snapshotId: "ws_000001",
    });
    await app.inject({
      method: "PUT",
      url: `/v1/groups/${groupId}/world-objects/${objectSha}`,
      headers: { "content-type": "application/octet-stream", ...hostHeaders(hostId, hostToken) },
      payload: content,
    });
    assert.equal(
      (await app.inject({
        method: "POST",
        url: `/v1/groups/${groupId}/world-backups/commit`,
        payload: { hostId, hostToken, hostGeneration: generation, manifest: first },
      })).statusCode,
      200,
    );

    const unchangedPlan = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/plan`,
      payload: {
        hostId,
        hostToken,
        hostGeneration: generation,
        parentSnapshotId: "ws_000001",
        objects: [{ sha256: objectSha, size: content.byteLength }],
      },
    });
    assert.equal(unchangedPlan.statusCode, 200, unchangedPlan.body);
    assert.equal(unchangedPlan.json<{ missingObjects: unknown[] }>().missingObjects.length, 0);

    const second = sampleWorldManifest({
      groupId,
      hostId,
      generation,
      sha256: objectSha,
      size: content.byteLength,
      snapshotId: "ws_000002",
    });
    second.parentSnapshotId = "ws_000001";
    second.createdAt = "2026-06-24T00:01:00.000Z";
    second.changedFileCount = 0;
    second.uploadedSize = 0;
    const commitSecond = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/commit`,
      payload: { hostId, hostToken, hostGeneration: generation, manifest: second },
    });
    assert.equal(commitSecond.statusCode, 200, commitSecond.body);

    const latest = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/world-backups/latest`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(latest.statusCode, 200, latest.body);
    assert.equal(latest.json<{ metadata: { snapshotId: string } }>().metadata.snapshotId, "ws_000002");
  } finally {
    await app.close();
  }
});

test("world backup commit rejects stale host generation", async (t) => {
  const app = await buildTestApp(t);
  try {
    const { groupId, hostId, hostToken, generation } = await createCurrentHost(app);
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const staleGeneration = generation - 1;
    const manifest = sampleWorldManifest({
      groupId,
      hostId,
      generation: staleGeneration,
      sha256: objectSha,
      size: content.byteLength,
      snapshotId: "ws_stale",
    });
    await app.inject({
      method: "PUT",
      url: `/v1/groups/${groupId}/world-objects/${objectSha}`,
      headers: { "content-type": "application/octet-stream", ...hostHeaders(hostId, hostToken) },
      payload: content,
    });
    const stale = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/commit`,
      payload: { hostId, hostToken, hostGeneration: staleGeneration, manifest },
    });
    assert.equal(stale.statusCode, 409, stale.body);
    assert.equal(stale.json<{ code: string }>().code, "stale_host_generation");
  } finally {
    await app.close();
  }
});

test("inconsistent latest world snapshot cannot be fetched for automatic restore", async (t) => {
  const app = await buildTestApp(t);
  try {
    const { groupId, hostId, hostToken, generation } = await createCurrentHost(app);
    const content = Buffer.from("unsafe online copy");
    const objectSha = sha256(content);
    const manifest = sampleWorldManifest({
      groupId,
      hostId,
      generation,
      sha256: objectSha,
      size: content.byteLength,
      snapshotId: "ws_inconsistent",
      consistent: false,
    });
    await app.inject({
      method: "PUT",
      url: `/v1/groups/${groupId}/world-objects/${objectSha}`,
      headers: {
        "content-type": "application/octet-stream",
        ...hostHeaders(hostId, hostToken),
      },
      payload: content,
    });
    const commit = await app.inject({
      method: "POST",
      url: `/v1/groups/${groupId}/world-backups/commit`,
      payload: { hostId, hostToken, hostGeneration: generation, manifest },
    });
    assert.equal(commit.statusCode, 200, commit.body);

    const latest = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/world-backups/latest?consistentOnly=true`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(latest.statusCode, 409);
    assert.equal(latest.json<{ code: string }>().code, "inconsistent_world_snapshot");
  } finally {
    await app.close();
  }
});

async function buildTestApp(t: TestContext): Promise<Awaited<ReturnType<typeof buildApp>>> {
  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-world-api-"));
  t.after(async () => {
    await rm(root, { force: true, recursive: true });
  });
  return buildApp({
    logger: false,
    storage: new LocalFilesystemStorage(root),
  });
}

async function createCurrentHost(app: Awaited<ReturnType<typeof buildApp>>): Promise<{
  groupId: string;
  accessKey: string;
  hostId: string;
  hostToken: string;
  generation: number;
}> {
  const createResponse = await app.inject({
    method: "POST",
    url: "/v1/groups",
    payload: { name: "Survival", ownerName: "Owner" },
  });
  const created = createResponse.json<{ groupId: string; accessKey: string }>();
  const host = await registerHost(app, created.groupId, created.accessKey, "Primary");
  await app.inject({
    method: "POST",
    url: "/v1/hosts/heartbeat",
    payload: {
      groupId: created.groupId,
      hostId: host.hostId,
      hostToken: host.hostToken,
      status: "standby",
    },
  });
  const check = await app.inject({
    method: "POST",
    url: `/v1/groups/${created.groupId}/election/check-timeout`,
    payload: {
      groupId: created.groupId,
      hostId: host.hostId,
      hostToken: host.hostToken,
    },
  });
  assert.equal(check.statusCode, 200, check.body);
  const poll = await app.inject({
    method: "POST",
    url: "/v1/hosts/takeover/poll",
    payload: {
      groupId: created.groupId,
      hostId: host.hostId,
      hostToken: host.hostToken,
    },
  });
  assert.equal(poll.statusCode, 200, poll.body);
  const assignment = poll.json<{
    assignment: { assignmentId: string; takeoverToken: string; currentHostGeneration: number };
  }>().assignment;
  const acceptPayload = {
    groupId: created.groupId,
    hostId: host.hostId,
    hostToken: host.hostToken,
    assignmentId: assignment.assignmentId,
    takeoverToken: assignment.takeoverToken,
  };
  assert.equal((await app.inject({ method: "POST", url: "/v1/hosts/takeover/accept", payload: acceptPayload })).statusCode, 200);
  const complete = await app.inject({ method: "POST", url: "/v1/hosts/takeover/complete", payload: acceptPayload });
  assert.equal(complete.statusCode, 200, complete.body);
  return {
    groupId: created.groupId,
    accessKey: created.accessKey,
    hostId: host.hostId,
    hostToken: host.hostToken,
    generation: assignment.currentHostGeneration + 1,
  };
}

async function registerHost(
  app: Awaited<ReturnType<typeof buildApp>>,
  groupId: string,
  accessKey: string,
  displayName: string,
): Promise<{ hostId: string; hostToken: string }> {
  const join = await app.inject({
    method: "POST",
    url: `/v1/groups/${groupId}/join`,
    payload: { accessKey, displayName },
  });
  assert.equal(join.statusCode, 200, join.body);
  const member = join.json<{ memberId: string }>();
  const register = await app.inject({
    method: "POST",
    url: "/v1/hosts/register",
    payload: {
      groupId,
      accessKey,
      memberId: member.memberId,
      deviceName: `${displayName}-PC`,
      platform: "windows",
      agentVersion: "v0.4.0-alpha1-test",
    },
  });
  assert.equal(register.statusCode, 200, register.body);
  return register.json<{ hostId: string; hostToken: string }>();
}

function sampleWorldManifest(input: {
  groupId: string;
  hostId: string;
  generation: number;
  sha256: string;
  size: number;
  snapshotId: string;
  consistent?: boolean;
}): WorldSnapshotManifest {
  return {
    schemaVersion: 1,
    snapshotId: input.snapshotId,
    groupId: input.groupId,
    sourceHostId: input.hostId,
    hostGeneration: input.generation,
    createdAt: "2026-06-24T00:00:00.000Z",
    consistent: input.consistent ?? true,
    logicalSize: input.size,
    uploadedSize: input.size,
    fileCount: 1,
    changedFileCount: 1,
    deletedFileCount: 0,
    files: [
      {
        path: "world/region/r.0.0.mca",
        size: input.size,
        sha256: input.sha256,
        objectId: `sha256:${input.sha256}`,
      },
    ],
    deletedPaths: [],
  };
}

function hostHeaders(hostId: string, hostToken: string): Record<string, string> {
  return {
    "x-acbh-host-id": hostId,
    "x-acbh-host-token": hostToken,
  };
}

function sha256(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}
