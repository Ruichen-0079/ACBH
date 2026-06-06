import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test, type TestContext } from "node:test";
import { buildApp } from "../src/app.js";
import { createInMemoryCoordinatorStore } from "../src/store.js";
import { LocalFilesystemStorage, type ArtifactManifest } from "../src/storage/index.js";

test("in-memory group join, host registration, heartbeat, and debug state", async (t) => {
  const app = await buildTestApp(t);

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

test("storage info endpoint reports local backend", async (t) => {
  const app = await buildTestApp(t);

  try {
    const response = await app.inject({
      method: "GET",
      url: "/v1/storage/info",
    });

    assert.equal(response.statusCode, 200);
    const info = response.json<{ backend: string; root: string; ready: boolean }>();
    assert.equal(info.backend, "local");
    assert.equal(typeof info.root, "string");
    assert.equal(info.ready, true);
  } finally {
    await app.close();
  }
});

test("artifact object and manifest push-pull flow", async (t) => {
  const app = await buildTestApp(t);

  try {
    const { groupId, hostId, hostToken } = await createJoinedHost(app);
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const manifest = sampleManifest({
      groupId,
      creatorHostId: hostId,
      sha256: objectSha,
      size: content.byteLength,
    });

    const unauthorized = await app.inject({
      method: "POST",
      url: "/v1/artifacts/objects",
      payload: {
        groupId,
        hostId,
        hostToken: "wrong",
        sha256: objectSha,
        contentBase64: content.toString("base64"),
      },
    });
    assert.equal(unauthorized.statusCode, 401);

    const badObject = await app.inject({
      method: "POST",
      url: "/v1/artifacts/objects",
      payload: {
        groupId,
        hostId,
        hostToken,
        sha256: sha256(Buffer.from("other")),
        contentBase64: content.toString("base64"),
      },
    });
    assert.equal(badObject.statusCode, 400);

    const malformedBase64 = await app.inject({
      method: "POST",
      url: "/v1/artifacts/objects",
      payload: {
        groupId,
        hostId,
        hostToken,
        sha256: objectSha,
        contentBase64: "not base64!?",
      },
    });
    assert.equal(malformedBase64.statusCode, 400);

    const emptyObjectSha = sha256(Buffer.alloc(0));
    const emptyObject = await app.inject({
      method: "POST",
      url: "/v1/artifacts/objects",
      payload: {
        groupId,
        hostId,
        hostToken,
        sha256: emptyObjectSha,
        contentBase64: "",
      },
    });
    assert.equal(emptyObject.statusCode, 200);
    assert.deepEqual(emptyObject.json(), {
      ok: true,
      sha256: emptyObjectSha,
      exists: false,
    });

    const missingManifest = await app.inject({
      method: "POST",
      url: "/v1/artifacts/manifests",
      payload: {
        groupId,
        hostId,
        hostToken,
        artifactKind: manifest.artifactKind,
        artifactId: manifest.artifactId,
        manifest,
      },
    });
    assert.equal(missingManifest.statusCode, 400);
    assert.match(missingManifest.body, /Missing object/);

    const uploadObject = await app.inject({
      method: "POST",
      url: "/v1/artifacts/objects",
      payload: {
        groupId,
        hostId,
        hostToken,
        sha256: objectSha,
        contentBase64: content.toString("base64"),
      },
    });
    assert.equal(uploadObject.statusCode, 200);
    assert.deepEqual(uploadObject.json(), {
      ok: true,
      sha256: objectSha,
      exists: false,
    });

    const duplicateObject = await app.inject({
      method: "POST",
      url: "/v1/artifacts/objects",
      payload: {
        groupId,
        hostId,
        hostToken,
        sha256: objectSha,
        contentBase64: content.toString("base64"),
      },
    });
    assert.equal(duplicateObject.statusCode, 200);
    assert.equal(duplicateObject.json<{ exists: boolean }>().exists, true);

    const uploadManifest = await app.inject({
      method: "POST",
      url: "/v1/artifacts/manifests",
      payload: {
        groupId,
        hostId,
        hostToken,
        artifactKind: manifest.artifactKind,
        artifactId: manifest.artifactId,
        manifest,
      },
    });
    assert.equal(uploadManifest.statusCode, 200);
    assert.deepEqual(uploadManifest.json(), {
      ok: true,
      artifactKind: "world-snapshot",
      artifactId: "snap_000001",
      status: "available",
    });

    const latest = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/artifacts/latest?artifactKind=world-snapshot`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(latest.statusCode, 200);
    assert.equal(latest.json<{ artifactId: string; status: string }>().artifactId, "snap_000001");
    assert.equal(latest.json<{ status: string }>().status, "available");

    const manifestDownload = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/artifacts/world-snapshot/snap_000001/manifest`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(manifestDownload.statusCode, 200);
    assert.deepEqual(manifestDownload.json<{ manifest: ArtifactManifest }>().manifest, manifest);

    const objectDownload = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/artifacts/objects/${objectSha}`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(objectDownload.statusCode, 200);
    assert.equal(objectDownload.headers["content-type"], "application/octet-stream");
    assert.equal(objectDownload.headers["content-length"], String(content.byteLength));
    assert.deepEqual(objectDownload.rawPayload, content);
  } finally {
    await app.close();
  }
});

test("artifact download endpoints require host auth", async (t) => {
  const app = await buildTestApp(t);

  try {
    const { groupId, hostId, hostToken } = await createJoinedHost(app);
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const manifest = sampleManifest({
      groupId,
      creatorHostId: hostId,
      sha256: objectSha,
      size: content.byteLength,
    });

    await uploadObject(app, { groupId, hostId, hostToken, sha256: objectSha, content });
    await uploadManifest(app, { groupId, hostId, hostToken, manifest });

    for (const url of [
      `/v1/groups/${groupId}/artifacts`,
      `/v1/groups/${groupId}/artifacts/latest?artifactKind=world-snapshot`,
      `/v1/groups/${groupId}/artifacts/world-snapshot/snap_000001/manifest`,
      `/v1/groups/${groupId}/artifacts/objects/${objectSha}`,
    ]) {
      const missingAuth = await app.inject({ method: "GET", url });
      assert.equal(missingAuth.statusCode, 401, url);

      const badAuth = await app.inject({
        method: "GET",
        url,
        headers: hostHeaders(hostId, "wrong"),
      });
      assert.equal(badAuth.statusCode, 401, url);
    }
  } finally {
    await app.close();
  }
});

test("only available newer artifacts become latest", async () => {
  const store = createInMemoryCoordinatorStore();
  const group = store.createGroup({ name: "Survival Server", ownerName: "Owner" });

  store.recordArtifact({
    groupId: group.groupId,
    artifactKind: "world-snapshot",
    artifactId: "snap_uploading",
    parentArtifactId: null,
    serverPackVersion: "pack_000001",
    creatorHostId: "host_123",
    createdAt: "2026-06-05T00:00:00Z",
    status: "uploading",
    manifestSha256: sha256(Buffer.from("uploading")),
    manifestObjectPath: "manifest-uploading",
    fileCount: 1,
    totalBytes: 10,
  });
  assert.throws(() => store.getLatestArtifact(group.groupId, "world-snapshot"));

  store.recordArtifact({
    groupId: group.groupId,
    artifactKind: "world-snapshot",
    artifactId: "snap_rejected",
    parentArtifactId: null,
    serverPackVersion: "pack_000001",
    creatorHostId: "host_123",
    createdAt: "2026-06-05T00:00:01Z",
    status: "rejected",
    manifestSha256: sha256(Buffer.from("rejected")),
    manifestObjectPath: "manifest-rejected",
    fileCount: 1,
    totalBytes: 10,
  });
  assert.throws(() => store.getLatestArtifact(group.groupId, "world-snapshot"));

  store.recordArtifact({
    groupId: group.groupId,
    artifactKind: "world-snapshot",
    artifactId: "snap_available",
    parentArtifactId: null,
    serverPackVersion: "pack_000001",
    creatorHostId: "host_123",
    createdAt: "2026-06-05T00:00:02Z",
    status: "available",
    manifestSha256: sha256(Buffer.from("available")),
    manifestObjectPath: "manifest-available",
    fileCount: 1,
    totalBytes: 10,
  });
  assert.equal(store.getLatestArtifact(group.groupId, "world-snapshot").artifactId, "snap_available");

  store.recordArtifact({
    groupId: group.groupId,
    artifactKind: "world-snapshot",
    artifactId: "snap_older_available",
    parentArtifactId: null,
    serverPackVersion: "pack_000001",
    creatorHostId: "host_123",
    createdAt: "2026-06-05T00:00:01Z",
    status: "available",
    manifestSha256: sha256(Buffer.from("older available")),
    manifestObjectPath: "manifest-older-available",
    fileCount: 1,
    totalBytes: 10,
  });
  assert.equal(store.getLatestArtifact(group.groupId, "world-snapshot").artifactId, "snap_available");
});

test("zero-byte non-deleted artifact can be uploaded and downloaded", async (t) => {
  const app = await buildTestApp(t);

  try {
    const { groupId, hostId, hostToken } = await createJoinedHost(app);
    const content = Buffer.alloc(0);
    const objectSha = sha256(content);
    const manifest = sampleManifest({
      groupId,
      creatorHostId: hostId,
      sha256: objectSha,
      size: 0,
    });
    manifest.files[0].path = "world/data/empty.dat";

    await uploadObject(app, { groupId, hostId, hostToken, sha256: objectSha, content });
    await uploadManifest(app, { groupId, hostId, hostToken, manifest });

    const objectDownload = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/artifacts/objects/${objectSha}`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(objectDownload.statusCode, 200);
    assert.equal(objectDownload.headers["content-length"], "0");
    assert.equal(objectDownload.rawPayload.byteLength, 0);
  } finally {
    await app.close();
  }
});

test("streaming object upload validates auth, hashes, limits, duplicates, and binary download", async (t) => {
  const app = await buildTestApp(t, { maxObjectBytes: 11 });

  try {
    const { groupId, hostId, hostToken } = await createJoinedHost(app);
    const content = Buffer.from("region data");
    const objectSha = sha256(content);
    const uploadHeaders = {
      "content-type": "application/octet-stream",
      "x-acbh-group-id": groupId,
      ...hostHeaders(hostId, hostToken),
    };

    const unauthorized = await app.inject({
      method: "PUT",
      url: `/v1/artifacts/objects/${objectSha}`,
      headers: {
        ...uploadHeaders,
        "x-acbh-host-token": "wrong",
      },
      payload: content,
    });
    assert.equal(unauthorized.statusCode, 401);

    const mismatch = await app.inject({
      method: "PUT",
      url: `/v1/artifacts/objects/${sha256(Buffer.from("other"))}`,
      headers: uploadHeaders,
      payload: content,
    });
    assert.equal(mismatch.statusCode, 400);
    assert.match(mismatch.body, /does not match sha256/);

    const tooLarge = await app.inject({
      method: "PUT",
      url: `/v1/artifacts/objects/${sha256(Buffer.alloc(12))}`,
      headers: uploadHeaders,
      payload: Buffer.alloc(12),
    });
    assert.equal(tooLarge.statusCode, 413);

    const uploaded = await app.inject({
      method: "PUT",
      url: `/v1/artifacts/objects/${objectSha}`,
      headers: uploadHeaders,
      payload: content,
    });
    assert.equal(uploaded.statusCode, 200, uploaded.body);
    assert.deepEqual(uploaded.json(), {
      ok: true,
      sha256: objectSha,
      exists: false,
      size: content.byteLength,
    });

    const duplicate = await app.inject({
      method: "PUT",
      url: `/v1/artifacts/objects/${objectSha}`,
      headers: uploadHeaders,
      payload: content,
    });
    assert.equal(duplicate.statusCode, 200, duplicate.body);
    assert.deepEqual(duplicate.json(), {
      ok: true,
      sha256: objectSha,
      exists: true,
      size: content.byteLength,
    });

    const download = await app.inject({
      method: "GET",
      url: `/v1/groups/${groupId}/artifacts/objects/${objectSha}`,
      headers: hostHeaders(hostId, hostToken),
    });
    assert.equal(download.statusCode, 200);
    assert.equal(download.headers["content-type"], "application/octet-stream");
    assert.deepEqual(download.rawPayload, content);

    const emptySha = sha256(Buffer.alloc(0));
    const emptyUpload = await app.inject({
      method: "PUT",
      url: `/v1/artifacts/objects/${emptySha}`,
      headers: uploadHeaders,
      payload: Buffer.alloc(0),
    });
    assert.equal(emptyUpload.statusCode, 200, emptyUpload.body);
    assert.deepEqual(emptyUpload.json(), {
      ok: true,
      sha256: emptySha,
      exists: false,
      size: 0,
    });
  } finally {
    await app.close();
  }
});

async function buildTestApp(
  t: TestContext,
  options: { maxObjectBytes?: number } = {},
): Promise<Awaited<ReturnType<typeof buildApp>>> {
  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-api-storage-"));
  t.after(async () => {
    await rm(root, { force: true, recursive: true });
  });
  return buildApp({
    logger: false,
    storage: new LocalFilesystemStorage(root),
    ...options,
  });
}

async function createJoinedHost(app: Awaited<ReturnType<typeof buildApp>>): Promise<{
  groupId: string;
  hostId: string;
  hostToken: string;
}> {
  const createResponse = await app.inject({
    method: "POST",
    url: "/v1/groups",
    payload: {
      name: "Survival Server",
      ownerName: "Owner",
    },
  });
  const created = createResponse.json<{ groupId: string; accessKey: string }>();

  const joinResponse = await app.inject({
    method: "POST",
    url: `/v1/groups/${created.groupId}/join`,
    payload: {
      accessKey: created.accessKey,
      displayName: "PlayerA",
    },
  });
  const joined = joinResponse.json<{ memberId: string }>();

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
  const registered = registerResponse.json<{ hostId: string; hostToken: string }>();

  return {
    groupId: created.groupId,
    hostId: registered.hostId,
    hostToken: registered.hostToken,
  };
}

function sampleManifest(input: {
  groupId: string;
  creatorHostId: string;
  sha256: string;
  size: number;
}): ArtifactManifest {
  return {
    manifestVersion: 1,
    artifactKind: "world-snapshot",
    artifactId: "snap_000001",
    groupId: input.groupId,
    createdAt: "2026-06-05T00:00:00Z",
    creatorHostId: input.creatorHostId,
    parentArtifactId: null,
    serverPackVersion: "pack_000001",
    files: [
      {
        path: "world/region/r.0.0.mca",
        class: "world-runtime",
        size: input.size,
        sha256: input.sha256,
        modifiedAt: "2026-06-05T00:00:00Z",
        deleted: false,
      },
      {
        path: "world/region/r.1.0.mca",
        class: "world-runtime",
        size: 0,
        sha256: "",
        modifiedAt: "2026-06-05T00:00:00Z",
        deleted: true,
      },
    ],
    summary: {
      includedFiles: 1,
      ignoredFiles: 0,
      unknownFiles: 0,
      deletedFiles: 1,
      totalBytes: input.size,
    },
  };
}

function sha256(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}

function hostHeaders(hostId: string, hostToken: string): Record<string, string> {
  return {
    "x-acbh-host-id": hostId,
    "x-acbh-host-token": hostToken,
  };
}

async function uploadObject(
  app: Awaited<ReturnType<typeof buildApp>>,
  input: { groupId: string; hostId: string; hostToken: string; sha256: string; content: Buffer },
): Promise<void> {
  const response = await app.inject({
    method: "POST",
    url: "/v1/artifacts/objects",
    payload: {
      groupId: input.groupId,
      hostId: input.hostId,
      hostToken: input.hostToken,
      sha256: input.sha256,
      contentBase64: input.content.toString("base64"),
    },
  });
  assert.equal(response.statusCode, 200, response.body);
}

async function uploadManifest(
  app: Awaited<ReturnType<typeof buildApp>>,
  input: { groupId: string; hostId: string; hostToken: string; manifest: ArtifactManifest },
): Promise<void> {
  const response = await app.inject({
    method: "POST",
    url: "/v1/artifacts/manifests",
    payload: {
      groupId: input.groupId,
      hostId: input.hostId,
      hostToken: input.hostToken,
      artifactKind: input.manifest.artifactKind,
      artifactId: input.manifest.artifactId,
      manifest: input.manifest,
    },
  });
  assert.equal(response.statusCode, 200, response.body);
}
