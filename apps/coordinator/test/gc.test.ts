import { createHash } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";
import { LocalFilesystemStorage } from "../src/storage/local.js";
import {
  createInMemoryCoordinatorStore,
  type GcBackend,
  GcBlockedError,
  StoreError,
  type ArtifactKind,
} from "../src/store.js";

function sha256hex(content: string): string {
  return createHash("sha256").update(content, "utf8").digest("hex");
}

function sha256raw(content: Buffer): string {
  return createHash("sha256").update(content).digest("hex");
}

function testGcBackend(storage: LocalFilesystemStorage): GcBackend {
  return {
    deleteManifest: async (p) => {
      await storage.deleteManifest({ groupId: p.groupId, artifactKind: p.artifactKind, artifactId: p.artifactId });
    },
    deleteObject: async (p) => {
      await storage.deleteObject({ groupId: p.groupId, sha256: p.sha256 });
    },
    listObjectSha256s: async (p) => {
      return storage.listObjectSha256s({ groupId: p.groupId });
    },
    readManifestFiles: async (p) => {
      const manifest = await storage.readManifest({
        groupId: p.groupId,
        artifactKind: p.artifactKind,
        artifactId: p.artifactId,
      });
      return (manifest.files ?? []).map((f) => ({ sha256: f.sha256, deleted: !!f.deleted }));
    },
  };
}

async function createStorage(): Promise<{ storage: LocalFilesystemStorage; root: string }> {
  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-gc-test-"));
  return { storage: new LocalFilesystemStorage(root), root };
}

async function saveArtifactMeta(
  storage: LocalFilesystemStorage,
  groupId: string,
  kind: ArtifactKind,
  artifactId: string,
  creatorHostId: string,
  createdAt?: string,
) {
  const content = Buffer.from("hello-world-object");
  const objectSha256 = sha256raw(content);
  const fileClass = kind === "world-snapshot" ? "world-runtime" : kind;
  const manifest = {
    manifestVersion: 1,
    artifactKind: kind,
    artifactId,
    groupId,
    createdAt: createdAt ?? "2026-06-01T00:00:00.000Z",
    creatorHostId,
    parentArtifactId: null,
    serverPackVersion: kind === "world-snapshot" ? "pack_000001" : null,
    files: [{ path: "test.file", class: fileClass, size: content.byteLength, sha256: objectSha256, modifiedAt: "2026-06-01T00:00:00.000Z", deleted: false }],
    summary: { includedFiles: 1, ignoredFiles: 0, unknownFiles: 0, deletedFiles: 0, totalBytes: content.byteLength },
  };
  await storage.saveObject({ groupId, sha256: objectSha256, content });
  await storage.saveManifest({ groupId, artifactKind: kind, artifactId, manifest });
  return manifest;
}

type TestHost = {
  groupId: string;
  hostId: string;
  hostToken: string;
};

function createTestHost(store: ReturnType<typeof createInMemoryCoordinatorStore>): TestHost & { accessKey: string } {
  const group = store.createGroup({ name: "GC", ownerName: "Owner" });
  const joined = store.joinGroup({ groupId: group.groupId, accessKey: group.accessKey, displayName: "Test" });
  const host = store.registerHost({
    groupId: group.groupId,
    accessKey: group.accessKey,
    memberId: joined.memberId,
    deviceName: "gc-host",
    platform: "test",
    agentVersion: "0.1.0",
  });
  return { groupId: group.groupId, hostId: host.hostId, hostToken: host.hostToken, accessKey: group.accessKey };
}

function addTestHost(store: ReturnType<typeof createInMemoryCoordinatorStore>, group: TestHost & { accessKey: string }): TestHost {
  const joined = store.joinGroup({ groupId: group.groupId, accessKey: group.accessKey, displayName: "Other" });
  const host = store.registerHost({
    groupId: group.groupId,
    accessKey: group.accessKey,
    memberId: joined.memberId,
    deviceName: "gc-host-b",
    platform: "test",
    agentVersion: "0.1.0",
  });
  return { groupId: group.groupId, hostId: host.hostId, hostToken: host.hostToken };
}

function recordArtifact(
  store: ReturnType<typeof createInMemoryCoordinatorStore>,
  groupId: string,
  host: TestHost,
  kind: ArtifactKind,
  artifactId: string,
  createdAt?: string,
  status: "available" | "rejected" = "available",
  serverPackVersion?: string,
) {
  store.recordArtifactFromHost({
    metadata: {
      groupId,
      artifactKind: kind,
      artifactId,
      parentArtifactId: null,
      serverPackVersion: serverPackVersion ?? (kind === "world-snapshot" ? "pack_000001" : null),
      creatorHostId: host.hostId,
      createdAt: createdAt ?? "2026-06-01T00:00:00.000Z",
      status,
      manifestSha256: sha256hex(artifactId),
      manifestObjectPath: `manifests/${kind}/${artifactId}.json`,
      fileCount: 1,
      totalBytes: 5,
    },
    hostId: host.hostId,
    hostToken: host.hostToken,
  });
}

async function prepareRetainedFailureScenario(
  store: ReturnType<typeof createInMemoryCoordinatorStore>,
  storage: LocalFilesystemStorage,
  host: TestHost,
) {
  const retained = await saveArtifactMeta(
    storage,
    host.groupId,
    "world-snapshot",
    "snap_retained",
    host.hostId,
    "2026-06-02T00:00:00Z",
  );
  recordArtifact(
    store,
    host.groupId,
    host,
    "world-snapshot",
    "snap_retained",
    "2026-06-02T00:00:00Z",
    "available",
  );
  await saveArtifactMeta(
    storage,
    host.groupId,
    "world-snapshot",
    "snap_candidate",
    host.hostId,
    "2026-06-01T00:00:00Z",
  );
  recordArtifact(
    store,
    host.groupId,
    host,
    "world-snapshot",
    "snap_candidate",
    "2026-06-01T00:00:00Z",
    "rejected",
  );

  const orphanContent = Buffer.from("blocked-orphan-data");
  const orphanSha256 = sha256raw(orphanContent);
  await storage.saveObject({ groupId: host.groupId, sha256: orphanSha256, content: orphanContent });

  return {
    retainedObjectSha256: retained.files[0].sha256,
    orphanSha256,
  };
}

function retainedManifestPath(root: string, groupId: string): string {
  return path.join(root, "groups", groupId, "world-snapshots", "snap_retained", "manifest.json");
}

test("dry-run does not delete any artifacts or objects", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_1", host.hostId, "2026-06-01T00:00:00Z");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_1", "2026-06-01T00:00:00Z", "rejected");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_2", "2026-06-02T00:00:00Z", "available");

    const allBefore = store.listArtifacts(host.groupId);
    assert.equal(allBefore.length, 2);

    const result = await store.gcArtifacts({
      groupId: host.groupId,
      dryRun: true,
      hostId: host.hostId,
      hostToken: host.hostToken,
      retentionPerKind: 1,
      minAgeMs: 0,
      backend: testGcBackend(storage),
    });

    assert.equal(result.dryRun, true);
    assert.equal(result.deletedObjectCount, 0);
    const deletedIds = result.deletedArtifacts.map((a) => a.artifactId);
    assert.ok(deletedIds.includes("snap_1"), "rejected old artifact should be candidate");
    assert.ok(!deletedIds.includes("snap_2"), "latest available artifact should not be candidate");

    const allAfter = store.listArtifacts(host.groupId);
    assert.equal(allAfter.length, 2, "dry-run must not delete artifacts from store");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("latest artifact per kind is never deleted", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_old", host.hostId, "2026-06-01T00:00:00Z");
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_new", host.hostId, "2026-06-02T00:00:00Z");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_old", "2026-06-01T00:00:00Z", "available");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_new", "2026-06-02T00:00:00Z", "available");

    const result = await store.gcArtifacts({
      groupId: host.groupId, dryRun: false, hostId: host.hostId, hostToken: host.hostToken,
      retentionPerKind: 1, minAgeMs: 0, backend: testGcBackend(storage),
    });

    assert.equal(result.deletedArtifacts.length, 1);
    assert.equal(result.deletedArtifacts[0].artifactId, "snap_old");
    assert.equal(store.getLatestArtifact(host.groupId, "world-snapshot").artifactId, "snap_new");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("recent N available artifacts per kind are retained", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 3, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    for (let i = 1; i <= 6; i++) {
      const ts = `2026-06-0${i}T00:00:00Z`;
      await saveArtifactMeta(storage, host.groupId, "world-snapshot", `snap_${i}`, host.hostId, ts);
      recordArtifact(store, host.groupId, host, "world-snapshot", `snap_${i}`, ts, "available");
    }

    const result = await store.gcArtifacts({
      groupId: host.groupId, dryRun: false, hostId: host.hostId, hostToken: host.hostToken,
      retentionPerKind: 3, minAgeMs: 0, backend: testGcBackend(storage),
    });

    assert.equal(result.deletedArtifacts.length, 3);
    const kept = store.listArtifacts(host.groupId, "world-snapshot").map((a) => a.artifactId);
    assert.equal(kept.length, 3);
    assert.ok(kept.includes("snap_6"), "newest should be kept");
    assert.ok(kept.includes("snap_5"), "second newest should be kept");
    assert.ok(kept.includes("snap_4"), "third newest should be kept");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("old rejected artifacts can be deleted", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_rejected", host.hostId, "2026-06-01T00:00:00Z");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_rejected", "2026-06-01T00:00:00Z", "rejected");
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_avail", host.hostId, "2026-06-02T00:00:00Z");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_avail", "2026-06-02T00:00:00Z", "available");

    const result = await store.gcArtifacts({
      groupId: host.groupId, dryRun: false, hostId: host.hostId, hostToken: host.hostToken,
      retentionPerKind: 1, minAgeMs: 0, backend: testGcBackend(storage),
    });

    const deleted = result.deletedArtifacts.map((a) => a.artifactId);
    assert.ok(deleted.includes("snap_rejected"), "old rejected should be deleted");
    assert.ok(!deleted.includes("snap_avail"), "available+latest should not be deleted");
    assert.equal(store.listArtifacts(host.groupId, "world-snapshot").length, 1);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("active takeover assignment artifacts are protected", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const hostA = createTestHost(store);
  const hostB = addTestHost(store, hostA);
  const { storage, root } = await createStorage();
  try {
    await saveArtifactMeta(storage, hostA.groupId, "world-snapshot", "snap_a", hostA.hostId, "2026-06-01T00:00:00Z");
    recordArtifact(store, hostA.groupId, hostA, "world-snapshot", "snap_a", "2026-06-01T00:00:00Z", "available");
    await saveArtifactMeta(storage, hostA.groupId, "world-snapshot", "snap_b", hostA.hostId, "2026-06-02T00:00:00Z");
    recordArtifact(store, hostA.groupId, hostA, "world-snapshot", "snap_b", "2026-06-02T00:00:00Z", "available");

    store.updateHeartbeat({
      groupId: hostA.groupId, hostId: hostA.hostId, hostToken: hostA.hostToken,
      status: "standby",
      latestLocalArtifacts: { "world-snapshot": "snap_b" },
      hostScoreHints: { javaAvailable: true },
    });
    store.updateHeartbeat({
      groupId: hostB.groupId, hostId: hostB.hostId, hostToken: hostB.hostToken,
      status: "standby", hostScoreHints: { javaAvailable: true },
    });
    store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);

    const election = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
    assert.ok(election.assignment, "election should create an assignment");

    const result = await store.gcArtifacts({
      groupId: hostA.groupId, dryRun: false, hostId: hostA.hostId, hostToken: hostA.hostToken,
      retentionPerKind: 1, minAgeMs: 0, backend: testGcBackend(storage),
    });

    const stillExists = store.listArtifacts(hostA.groupId, "world-snapshot");
    const stillIds = stillExists.map((a) => a.artifactId);
    assert.ok(stillIds.includes("snap_b"), "snap_b is latest + in active assignment, should be protected");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("uploading artifacts are never deleted", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_uploading", "2026-06-01T00:00:00Z", "uploading" as any);
    store.recordArtifactFromHost({
      metadata: {
        groupId: host.groupId, artifactKind: "world-snapshot", artifactId: "snap_uploading",
        parentArtifactId: null, serverPackVersion: "pack_000001", creatorHostId: host.hostId,
        createdAt: "2026-06-01T00:00:00Z", status: "uploading" as any,
        manifestSha256: sha256hex("uploading"), manifestObjectPath: "x", fileCount: 1, totalBytes: 1,
      },
      hostId: host.hostId,
      hostToken: host.hostToken,
    });
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_avail", host.hostId, "2026-06-02T00:00:00Z");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_avail", "2026-06-02T00:00:00Z", "available");

    const result = await store.gcArtifacts({
      groupId: host.groupId, dryRun: false, hostId: host.hostId, hostToken: host.hostToken,
      retentionPerKind: 1, minAgeMs: 0, backend: testGcBackend(storage),
    });

    const deleted = result.deletedArtifacts.map((a) => a.artifactId);
    assert.ok(!deleted.includes("snap_uploading"), "uploading artifact must never be deleted");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("orphan object blobs are deleted when unreferenced", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    const orphanContent = Buffer.from("orphan-data");
    const objDelete = sha256raw(orphanContent);
    await saveArtifactMeta(storage, host.groupId, "world-snapshot", "snap_1", host.hostId, "2026-06-01T00:00:00Z");
    recordArtifact(store, host.groupId, host, "world-snapshot", "snap_1", "2026-06-01T00:00:00Z", "available");
    await storage.saveObject({ groupId: host.groupId, sha256: objDelete, content: orphanContent });

    assert.ok(await storage.objectExists({ groupId: host.groupId, sha256: objDelete }));

    await store.gcArtifacts({
      groupId: host.groupId, dryRun: false, hostId: host.hostId, hostToken: host.hostToken,
      retentionPerKind: 5, minAgeMs: 0, backend: testGcBackend(storage),
    });

    assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objDelete }), false, "orphan object should be deleted");
    const keptSha256 = sha256raw(Buffer.from("hello-world-object"));
    assert.ok(await storage.objectExists({ groupId: host.groupId, sha256: keptSha256 }), "referenced object should survive");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("missing retained manifest blocks dry-run and preserves objects", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    const objects = await prepareRetainedFailureScenario(store, storage, host);
    await storage.deleteManifest({
      groupId: host.groupId,
      artifactKind: "world-snapshot",
      artifactId: "snap_retained",
    });

    const result = await store.gcArtifacts({
      groupId: host.groupId,
      dryRun: true,
      hostId: host.hostId,
      hostToken: host.hostToken,
      retentionPerKind: 1,
      minAgeMs: 0,
      backend: testGcBackend(storage),
    });

    assert.equal(result.blocked, true);
    assert.deepEqual(result.blockers, [{
      groupId: host.groupId,
      artifactKind: "world-snapshot",
      artifactId: "snap_retained",
      reason: "manifest does not exist",
    }]);
    assert.deepEqual(result.deletedArtifacts.map((artifact) => artifact.artifactId), ["snap_candidate"]);
    assert.equal(result.deletedObjectCount, 0);
    assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objects.retainedObjectSha256 }), true);
    assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objects.orphanSha256 }), true);
    assert.equal(store.listArtifacts(host.groupId).length, 2);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("corrupt retained manifest blocks non-dry-run before any deletion", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    const objects = await prepareRetainedFailureScenario(store, storage, host);
    await writeFile(retainedManifestPath(root, host.groupId), "{not-json", "utf8");

    await assert.rejects(
      store.gcArtifacts({
        groupId: host.groupId,
        dryRun: false,
        hostId: host.hostId,
        hostToken: host.hostToken,
        retentionPerKind: 1,
        minAgeMs: 0,
        backend: testGcBackend(storage),
      }),
      (error: unknown) =>
        error instanceof GcBlockedError &&
        error.statusCode === 409 &&
        error.blockers[0]?.reason === "manifest contains invalid JSON",
    );

    assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objects.retainedObjectSha256 }), true);
    assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objects.orphanSha256 }), true);
    await storage.readManifest({
      groupId: host.groupId,
      artifactKind: "world-snapshot",
      artifactId: "snap_candidate",
    });
    assert.equal(store.listArtifacts(host.groupId).length, 2);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("retained manifest storage error blocks GC without leaking raw paths", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    const objects = await prepareRetainedFailureScenario(store, storage, host);
    const backend = testGcBackend(storage);
    backend.readManifestFiles = async (params) => {
      if (params.artifactId === "snap_retained") {
        throw new Error("EACCES: permission denied, open 'C:\\secret\\manifest.json'");
      }
      const manifest = await storage.readManifest(params);
      return manifest.files.map((file) => ({ sha256: file.sha256, deleted: file.deleted }));
    };

    await assert.rejects(
      store.gcArtifacts({
        groupId: host.groupId,
        dryRun: false,
        hostId: host.hostId,
        hostToken: host.hostToken,
        retentionPerKind: 1,
        minAgeMs: 0,
        backend,
      }),
      (error: unknown) => {
        assert.ok(error instanceof GcBlockedError);
        assert.equal(error.blockers[0]?.reason, "manifest storage read failed");
        assert.ok(!error.message.includes("C:\\secret"));
        return true;
      },
    );

    assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objects.orphanSha256 }), true);
    assert.equal(store.listArtifacts(host.groupId).length, 2);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("non-dry-run GC route returns structured 409 for a retained manifest failure", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const host = createTestHost(store);
  const { storage, root } = await createStorage();
  try {
    const objects = await prepareRetainedFailureScenario(store, storage, host);
    await storage.deleteManifest({
      groupId: host.groupId,
      artifactKind: "world-snapshot",
      artifactId: "snap_retained",
    });
    const app = await buildApp({ logger: false, store, storage });
    try {
      const response = await app.inject({
        method: "POST",
        url: `/v1/groups/${host.groupId}/artifacts/gc`,
        headers: {
          "x-acbh-host-id": host.hostId,
          "x-acbh-host-token": host.hostToken,
        },
        payload: { dryRun: false, retentionPerKind: 1, minAgeMs: 1 },
      });

      assert.equal(response.statusCode, 409, response.body);
      const body = response.json();
      assert.equal(body.blocked, true);
      assert.deepEqual(body.blockers, [{
        groupId: host.groupId,
        artifactKind: "world-snapshot",
        artifactId: "snap_retained",
        reason: "manifest does not exist",
      }]);
      assert.ok(!response.body.includes(host.hostToken));
      assert.equal(await storage.objectExists({ groupId: host.groupId, sha256: objects.orphanSha256 }), true);
      assert.equal(store.listArtifacts(host.groupId).length, 2);
    } finally {
      await app.close();
    }
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("stale host GC returns 403", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const hostA = createTestHost(store);
  const hostB = addTestHost(store, hostA);
  store.updateHeartbeat({
    groupId: hostA.groupId, hostId: hostA.hostId, hostToken: hostA.hostToken,
    status: "standby", hostScoreHints: { javaAvailable: true },
  });
  store.updateHeartbeat({
    groupId: hostA.groupId, hostId: hostB.hostId, hostToken: hostB.hostToken,
    status: "standby", hostScoreHints: { javaAvailable: true },
  });
  store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);
  const election = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
  const assignmentId = election.assignment!.assignmentId;
  const polled = store.pollTakeover(hostA);
  const token = polled.assignment!.takeoverToken!;
  store.acceptTakeover({ ...hostA, assignmentId, takeoverToken: token });
  store.completeTakeover({ ...hostA, assignmentId, takeoverToken: token });

  const { storage, root } = await createStorage();
  try {
    await assert.rejects(
      store.gcArtifacts({
        groupId: hostA.groupId, dryRun: false, hostId: hostB.hostId, hostToken: hostB.hostToken,
        retentionPerKind: 1, minAgeMs: 0, backend: testGcBackend(storage),
      }),
      (err: unknown) => err instanceof StoreError && (err as StoreError).statusCode === 403,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("stale generation GC returns 409", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const hostA = createTestHost(store);
  const hostB = addTestHost(store, hostA);
  store.updateHeartbeat({
    groupId: hostA.groupId, hostId: hostA.hostId, hostToken: hostA.hostToken,
    status: "standby", hostScoreHints: { javaAvailable: true },
  });
  store.updateHeartbeat({
    groupId: hostA.groupId, hostId: hostB.hostId, hostToken: hostB.hostToken,
    status: "standby", hostScoreHints: { javaAvailable: true },
  });
  store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);
  const election = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
  const assignmentId = election.assignment!.assignmentId;
  const polled = store.pollTakeover(hostA);
  const token = polled.assignment!.takeoverToken!;
  store.acceptTakeover({ ...hostA, assignmentId, takeoverToken: token });
  store.completeTakeover({ ...hostA, assignmentId, takeoverToken: token });

  const { storage, root } = await createStorage();
  try {
    await assert.rejects(
      store.gcArtifacts({
        groupId: hostA.groupId, dryRun: false, hostId: hostA.hostId, hostToken: hostA.hostToken,
        currentHostGeneration: 0, retentionPerKind: 1, minAgeMs: 0, backend: testGcBackend(storage),
      }),
      (err: unknown) => err instanceof StoreError && (err as StoreError).statusCode === 409,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("GC route uses header auth and generation fence", async () => {
  const store = createInMemoryCoordinatorStore({ retentionPerKind: 1, gcMinAgeMs: 0 });
  const hostA = createTestHost(store);
  const hostB = addTestHost(store, hostA);
  store.updateHeartbeat({
    groupId: hostA.groupId, hostId: hostA.hostId, hostToken: hostA.hostToken,
    status: "standby", hostScoreHints: { javaAvailable: true },
  });
  store.updateHeartbeat({
    groupId: hostA.groupId, hostId: hostB.hostId, hostToken: hostB.hostToken,
    status: "standby", hostScoreHints: { javaAvailable: true },
  });
  store.setHostManualPriority(hostA.groupId, hostA.hostId, 20);
  const election = store.runElection({ groupId: hostA.groupId, reason: "no-current-host" });
  const assignmentId = election.assignment!.assignmentId;
  const polled = store.pollTakeover(hostA);
  const token = polled.assignment!.takeoverToken!;
  store.acceptTakeover({ ...hostA, assignmentId, takeoverToken: token });
  store.completeTakeover({ ...hostA, assignmentId, takeoverToken: token });

  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-gc-route-"));
  try {
    const storage = new LocalFilesystemStorage(root);
    const app = await buildApp({ logger: false, store, storage });
    try {
      const staleHost = await app.inject({
        method: "POST",
        url: `/v1/groups/${hostA.groupId}/artifacts/gc`,
        headers: {
          "x-acbh-host-id": hostB.hostId,
          "x-acbh-host-token": hostB.hostToken,
          "x-acbh-host-generation": "1",
        },
        payload: { dryRun: true },
      });
      assert.equal(staleHost.statusCode, 403, staleHost.body);

      const noAuth = await app.inject({
        method: "POST",
        url: `/v1/groups/${hostA.groupId}/artifacts/gc`,
        payload: { dryRun: true },
      });
      assert.equal(noAuth.statusCode, 401, noAuth.body);

      const staleGen = await app.inject({
        method: "POST",
        url: `/v1/groups/${hostA.groupId}/artifacts/gc`,
        headers: {
          "x-acbh-host-id": hostA.hostId,
          "x-acbh-host-token": hostA.hostToken,
          "x-acbh-host-generation": "0",
        },
        payload: { dryRun: true },
      });
      assert.equal(staleGen.statusCode, 409, staleGen.body);

      const ok = await app.inject({
        method: "POST",
        url: `/v1/groups/${hostA.groupId}/artifacts/gc`,
        headers: {
          "x-acbh-host-id": hostA.hostId,
          "x-acbh-host-token": hostA.hostToken,
          "x-acbh-host-generation": "1",
        },
        payload: { dryRun: true },
      });
      assert.equal(ok.statusCode, 200, ok.body);
    } finally {
      await app.close();
    }
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
