import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test, type TestContext } from "node:test";
import { buildApp } from "../src/app.js";
import { createInMemoryCoordinatorStore } from "../src/store.js";
import { LocalFilesystemStorage, type ArtifactManifest } from "../src/storage/index.js";

test("host registration and group state require group-scoped authentication", async (t) => {
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

    const anonymousRegister = await app.inject({
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
    assert.equal(anonymousRegister.statusCode, 401);

    const wrongKey = "wrong-access-key";
    const deniedRegister = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: {
        groupId: created.groupId,
        accessKey: wrongKey,
        memberId: joined.memberId,
        deviceName: "PlayerA-PC",
        platform: "windows",
        agentVersion: "0.1.0",
      },
    });
    assert.equal(deniedRegister.statusCode, 401);
    assert.equal(deniedRegister.body.includes(wrongKey), false);

    const registerResponse = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: {
        groupId: created.groupId,
        accessKey: created.accessKey,
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

    const anonymousState = await app.inject({
      method: "GET",
      url: `/v1/groups/${created.groupId}/state`,
    });
    assert.equal(anonymousState.statusCode, 401);
    assert.equal(anonymousState.body.includes(created.accessKey), false);
    assert.equal(anonymousState.body.includes(registered.hostToken), false);

    const stateResponse = await app.inject({
      method: "GET",
      url: `/v1/groups/${created.groupId}/state`,
      headers: {
        "x-acbh-access-key": created.accessKey,
      },
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

    const hostAuthenticatedState = await app.inject({
      method: "GET",
      url: `/v1/groups/${created.groupId}/state`,
      headers: hostHeaders(registered.hostId, registered.hostToken),
    });
    assert.equal(hostAuthenticatedState.statusCode, 200);

    const otherGroup = (
      await app.inject({
        method: "POST",
        url: "/v1/groups",
        payload: {
          name: "Other Server",
          ownerName: "Other Owner",
        },
      })
    ).json<{ groupId: string; accessKey: string }>();

    const crossGroupRegister = await app.inject({
      method: "POST",
      url: "/v1/hosts/register",
      payload: {
        groupId: created.groupId,
        accessKey: otherGroup.accessKey,
        memberId: joined.memberId,
        deviceName: "Cross-group-PC",
        platform: "windows",
        agentVersion: "0.1.0",
      },
    });
    assert.equal(crossGroupRegister.statusCode, 401);
    assert.equal(crossGroupRegister.body.includes(otherGroup.accessKey), false);
    assert.equal(crossGroupRegister.body.includes(registered.hostToken), false);

    const crossGroupState = await app.inject({
      method: "GET",
      url: `/v1/groups/${created.groupId}/state`,
      headers: {
        "x-acbh-access-key": otherGroup.accessKey,
      },
    });
    assert.equal(crossGroupState.statusCode, 401);
    assert.equal(crossGroupState.body.includes(otherGroup.accessKey), false);
    assert.equal(crossGroupState.body.includes(registered.hostToken), false);
  } finally {
    await app.close();
  }
});

test("invite codes register joined hosts and support revoke", async (t) => {
  const app = await buildTestApp(t);

  try {
    const createdResponse = await app.inject({
      method: "POST",
      url: "/v1/groups",
      payload: {
        name: "Invite Server",
        ownerName: "Owner",
      },
    });
    assert.equal(createdResponse.statusCode, 200);
    const created = createdResponse.json<{ groupId: string; accessKey: string }>();

    const deniedCreate = await app.inject({
      method: "POST",
      url: `/v1/groups/${created.groupId}/invites`,
      payload: {
        accessKey: "wrong",
      },
    });
    assert.equal(deniedCreate.statusCode, 401);
    assert.equal(deniedCreate.body.includes(created.accessKey), false);

    const inviteResponse = await app.inject({
      method: "POST",
      url: `/v1/groups/${created.groupId}/invites`,
      payload: {
        accessKey: created.accessKey,
        expiresInSeconds: 120,
        oneTime: true,
      },
    });
    assert.equal(inviteResponse.statusCode, 200, inviteResponse.body);
    const invite = inviteResponse.json<{
      inviteId: string;
      inviteCode: string;
      groupId: string;
      oneTime: boolean;
    }>();
    assert.match(invite.inviteId, /^inv_/);
    assert.match(invite.inviteCode, /^ACBH-[0-9A-F]{6}-[0-9A-F]{6}$/);
    assert.equal(invite.groupId, created.groupId);
    assert.equal(invite.oneTime, true);
    assert.equal(inviteResponse.body.includes(created.accessKey), false);

    const joinedResponse = await app.inject({
      method: "POST",
      url: "/v1/invites/join",
      payload: {
        inviteCode: invite.inviteCode,
        displayName: "PlayerA",
        deviceName: "PlayerA-PC",
        platform: "windows",
        agentVersion: "0.3.3-test",
      },
    });
    assert.equal(joinedResponse.statusCode, 200, joinedResponse.body);
    const joined = joinedResponse.json<{ groupId: string; memberId: string; hostId: string; hostToken: string }>();
    assert.equal(joined.groupId, created.groupId);
    assert.match(joined.hostToken, /^ht_/);
    assert.equal(joinedResponse.body.includes(invite.inviteCode), false);

    const heartbeat = await app.inject({
      method: "POST",
      url: "/v1/hosts/heartbeat",
      payload: {
        groupId: joined.groupId,
        hostId: joined.hostId,
        hostToken: joined.hostToken,
        status: "standby",
      },
    });
    assert.equal(heartbeat.statusCode, 200, heartbeat.body);

    const reuse = await app.inject({
      method: "POST",
      url: "/v1/invites/join",
      payload: {
        inviteCode: invite.inviteCode,
        displayName: "PlayerB",
        deviceName: "PlayerB-PC",
        platform: "windows",
        agentVersion: "0.3.3-test",
      },
    });
    assert.equal(reuse.statusCode, 401);

    const reusableInvite = (
      await app.inject({
        method: "POST",
        url: `/v1/groups/${created.groupId}/invites`,
        payload: {
          accessKey: created.accessKey,
          expiresInSeconds: 120,
          oneTime: false,
        },
      })
    ).json<{ inviteId: string; inviteCode: string; oneTime: boolean }>();
    assert.equal(reusableInvite.oneTime, false);

    for (const player of ["PlayerC", "PlayerD"]) {
      const response = await app.inject({
        method: "POST",
        url: "/v1/invites/join",
        payload: {
          inviteCode: reusableInvite.inviteCode,
          displayName: player,
          deviceName: `${player}-PC`,
          platform: "windows",
          agentVersion: "0.3.3-test",
        },
      });
      assert.equal(response.statusCode, 200, response.body);
    }

    const revoke = await app.inject({
      method: "POST",
      url: `/v1/groups/${created.groupId}/invites/revoke`,
      payload: {
        accessKey: created.accessKey,
        inviteId: reusableInvite.inviteId,
      },
    });
    assert.equal(revoke.statusCode, 200, revoke.body);

    const revokedJoin = await app.inject({
      method: "POST",
      url: "/v1/invites/join",
      payload: {
        inviteCode: reusableInvite.inviteCode,
        displayName: "PlayerE",
        deviceName: "PlayerE-PC",
        platform: "windows",
        agentVersion: "0.3.3-test",
      },
    });
    assert.equal(revokedJoin.statusCode, 401);
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

test("health and capabilities expose alpha6 protocol compatibility", async (t) => {
  const app = await buildTestApp(t);

  try {
    const health = await app.inject({ method: "GET", url: "/health" });
    assert.equal(health.statusCode, 200, health.body);
    const healthBody = health.json<{ version: string; coordinatorVersion: string; buildCommit: string; protocolVersion: number }>();
    assert.equal(healthBody.version, "v0.5.1-public-relay-hotfix");
    assert.equal(healthBody.coordinatorVersion, "v0.5.1-public-relay-hotfix");
    assert.equal(healthBody.buildCommit, "dev");
    assert.equal(healthBody.protocolVersion, 2);

    const version = await app.inject({ method: "GET", url: "/version" });
    assert.equal(version.statusCode, 200, version.body);
    const versionBody = version.json<{ version: string; buildCommit: string; protocolVersion: number }>();
    assert.equal(versionBody.version, "v0.5.1-public-relay-hotfix");
    assert.equal(versionBody.buildCommit, "dev");
    assert.equal(versionBody.protocolVersion, 2);

    const capabilities = await app.inject({ method: "GET", url: "/v1/capabilities" });
    assert.equal(capabilities.statusCode, 200, capabilities.body);
    const body = capabilities.json<{ capabilities: string[]; coordinatorVersion: string; buildCommit: string; protocolVersion: number }>();
    assert.equal(body.coordinatorVersion, "v0.5.1-public-relay-hotfix");
    assert.equal(body.buildCommit, "dev");
    assert.equal(body.protocolVersion, 2);
    assert.ok(body.capabilities.includes("lease_renew_v1"));
    assert.ok(body.capabilities.includes("world_backup_v1"));
  } finally {
    await app.close();
  }
});

test("world backup list exposes success metadata for committed snapshots", async (t) => {
  const store = createInMemoryCoordinatorStore();
  const app = await buildTestApp(t, { store });

  try {
    const group = store.createGroup({ name: "World Backup Server", ownerName: "Owner" });
    const joined = store.joinGroup({ groupId: group.groupId, accessKey: group.accessKey, displayName: "PlayerA" });
    const host = store.registerHost({
      groupId: group.groupId,
      accessKey: group.accessKey,
      memberId: joined.memberId,
      deviceName: "PlayerA-PC",
      platform: "windows",
      agentVersion: "0.4.0-alpha6-test",
    });
    const lease = store.ensureActiveLease({ groupId: group.groupId, hostId: host.hostId, hostToken: host.hostToken });

    const content = Buffer.from("level data");
    const objectSha = sha256(content);
    const upload = await app.inject({
      method: "PUT",
      url: `/v1/groups/${group.groupId}/world-objects/${objectSha}`,
      headers: {
        "content-type": "application/octet-stream",
        "x-acbh-group-id": group.groupId,
        ...hostHeaders(host.hostId, host.hostToken),
      },
      payload: content,
    });
    assert.equal(upload.statusCode, 200, upload.body);

    const manifest = {
      schemaVersion: 1,
      snapshotId: "ws_meta_001",
      groupId: group.groupId,
      sourceHostId: host.hostId,
      hostGeneration: lease.lease.generation,
      createdAt: "2026-06-30T00:00:00.000Z",
      consistent: true,
      logicalSize: content.byteLength,
      uploadedSize: content.byteLength,
      fileCount: 1,
      changedFileCount: 1,
      deletedFileCount: 0,
      files: [
        {
          path: "world/level.dat",
          size: content.byteLength,
          sha256: objectSha,
          objectId: `sha256:${objectSha}`,
        },
      ],
      deletedPaths: [],
    };

    const commit = await app.inject({
      method: "POST",
      url: `/v1/groups/${group.groupId}/world-backups/commit`,
      payload: {
        hostId: host.hostId,
        hostToken: host.hostToken,
        hostGeneration: lease.lease.generation,
        manifest,
      },
    });
    assert.equal(commit.statusCode, 200, commit.body);

    const list = await app.inject({
      method: "GET",
      url: `/v1/groups/${group.groupId}/world-backups`,
      headers: hostHeaders(host.hostId, host.hostToken),
    });
    assert.equal(list.statusCode, 200, list.body);
    const snapshots = list.json<{ snapshots: Array<Record<string, unknown>> }>().snapshots;
    assert.equal(snapshots.length, 1);
    assert.equal(snapshots[0].snapshotId, "ws_meta_001");
    assert.equal(snapshots[0].status, "success");
    assert.equal(snapshots[0].createdAt, "2026-06-30T00:00:00.000Z");
    assert.equal(snapshots[0].profileId, "minecraft-migratable");
    assert.equal(snapshots[0].fileCount, 1);
    assert.equal(snapshots[0].logicalSize, content.byteLength);
    assert.equal(snapshots[0].uploadedSize, content.byteLength);
    assert.equal(snapshots[0].canRestore, true);
    assert.equal(snapshots[0].canDownload, true);
  } finally {
    await app.close();
  }
});

test("bootstrap manifest exposes locally installed runtime packages", async (t) => {
  const packageDir = await mkdtemp(path.join(os.tmpdir(), "acbh-bootstrap-packages-"));
  const previous = process.env.ACBH_BOOTSTRAP_PACKAGE_DIR;
  process.env.ACBH_BOOTSTRAP_PACKAGE_DIR = packageDir;
  t.after(async () => {
    if (previous === undefined) {
      delete process.env.ACBH_BOOTSTRAP_PACKAGE_DIR;
    } else {
      process.env.ACBH_BOOTSTRAP_PACKAGE_DIR = previous;
    }
    await rm(packageDir, { force: true, recursive: true });
  });

  const content = Buffer.from("runtime package");
  await writeFile(path.join(packageDir, "acbh-runtime-base-windows-amd64.zip"), content);
  await writeFile(path.join(packageDir, "acbh-runtime-base-windows-amd64.zip.sig"), "test-signature\n");
  const app = await buildTestApp(t);

  try {
    const manifestResponse = await app.inject({
      method: "GET",
      url: "/v1/bootstrap/manifest",
      headers: { host: "example.test" },
    });
    assert.equal(manifestResponse.statusCode, 200, manifestResponse.body);
    const runtimePackage = manifestResponse
      .json<{
        packages: Array<{
          id: string;
          available: boolean;
          size: number;
          sha256: string;
          signature: string;
          url: string | null;
        }>;
      }>()
      .packages.find((pkg) => pkg.id === "acbh-runtime-base-windows-amd64");
    assert.ok(runtimePackage);
    assert.equal(runtimePackage.available, true);
    assert.equal(runtimePackage.size, content.byteLength);
    assert.equal(runtimePackage.sha256, sha256(content));
    assert.equal(runtimePackage.signature, "test-signature");
    assert.match(runtimePackage.url ?? "", /\/v1\/bootstrap\/packages\/acbh-runtime-base-windows-amd64\.zip$/);

    const downloadResponse = await app.inject({
      method: "GET",
      url: "/v1/bootstrap/packages/acbh-runtime-base-windows-amd64.zip",
    });
    assert.equal(downloadResponse.statusCode, 200, downloadResponse.body);
    assert.deepEqual(downloadResponse.rawPayload, content);
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
  options: { maxObjectBytes?: number; store?: ReturnType<typeof createInMemoryCoordinatorStore> } = {},
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
      accessKey: created.accessKey,
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
