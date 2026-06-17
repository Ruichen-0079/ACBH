import { createHash } from "node:crypto";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  LocalFilesystemStorage,
  StorageNotFoundError,
  StorageValidationError,
  createLocalFilesystemStorageFromEnv,
  type ArtifactManifest,
} from "../src/storage/index.js";

test("writes, checks, and reads content-addressed objects", async () => {
  const { storage, root } = await testStorage();
  try {
    const content = Buffer.from("hello storage");
    const sha256 = hash(content);

    assert.equal(await storage.objectExists({ groupId: "grp_123", sha256 }), false);
    await storage.saveObject({ groupId: "grp_123", sha256, content });

    assert.equal(await storage.objectExists({ groupId: "grp_123", sha256 }), true);
    assert.deepEqual(await storage.readObject({ groupId: "grp_123", sha256 }), content);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects object content when sha256 does not match", async () => {
  const { storage, root } = await testStorage();
  try {
    await assert.rejects(
      storage.saveObject({
        groupId: "grp_123",
        sha256: hash(Buffer.from("expected")),
        content: Buffer.from("actual"),
      }),
      StorageValidationError,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("streams object writes and reads without buffering in the storage interface", async () => {
  const { storage, root } = await testStorage();
  try {
    const content = Buffer.from("streamed region data");
    const sha256 = hash(content);
    const saved = await storage.saveObjectFromStream({
      groupId: "grp_123",
      sha256,
      stream: Readable.from([content.subarray(0, 8), content.subarray(8)]),
      maxBytes: content.byteLength,
    });
    assert.equal(saved.size, content.byteLength);

    const opened = await storage.createObjectReadStream({ groupId: "grp_123", sha256 });
    const chunks: Buffer[] = [];
    for await (const chunk of opened.stream) {
      chunks.push(Buffer.from(chunk));
    }
    assert.equal(opened.size, content.byteLength);
    assert.deepEqual(Buffer.concat(chunks), content);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("saves and reads world snapshot manifests", async () => {
  const { storage, root } = await testStorage();
  try {
    const manifest = sampleManifest("world-snapshot", "snap_001", "pack_001");

    await storage.saveManifest({
      groupId: manifest.groupId,
      artifactKind: manifest.artifactKind,
      artifactId: manifest.artifactId,
      manifest,
    });

    assert.deepEqual(
      await storage.readManifest({
        groupId: manifest.groupId,
        artifactKind: manifest.artifactKind,
        artifactId: manifest.artifactId,
      }),
      manifest,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("manifest commit is atomic and cleans temporary files on failure", async () => {
  const { storage, root } = await testStorage();
  try {
    const original = sampleManifest("world-snapshot", "snap_atomic", "pack_001");
    await storage.saveManifest({
      groupId: original.groupId,
      artifactKind: original.artifactKind,
      artifactId: original.artifactId,
      manifest: original,
    });

    const replacement = sampleManifest("world-snapshot", "snap_atomic", "pack_001", {
      createdAt: "2026-06-05T00:01:00.000Z",
    });
    await assert.rejects(
      storage.saveManifest({
        groupId: replacement.groupId,
        artifactKind: replacement.artifactKind,
        artifactId: replacement.artifactId,
        manifest: replacement,
        beforeCommit: () => {
          throw new Error("fence changed before commit");
        },
      }),
      /fence changed before commit/,
    );

    assert.deepEqual(
      await storage.readManifest({
        groupId: original.groupId,
        artifactKind: original.artifactKind,
        artifactId: original.artifactId,
      }),
      original,
    );
    const artifactDirectory = path.join(
      root,
      "groups",
      original.groupId,
      "world-snapshots",
      original.artifactId,
    );
    assert.deepEqual(await readdir(artifactDirectory), ["manifest.json"]);

    await storage.saveManifest({
      groupId: replacement.groupId,
      artifactKind: replacement.artifactKind,
      artifactId: replacement.artifactId,
      manifest: replacement,
    });
    assert.deepEqual(
      await storage.readManifest({
        groupId: replacement.groupId,
        artifactKind: replacement.artifactKind,
        artifactId: replacement.artifactId,
      }),
      replacement,
    );
    assert.deepEqual(await readdir(artifactDirectory), ["manifest.json"]);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("saves and reads server pack manifests", async () => {
  const { storage, root } = await testStorage();
  try {
    const manifest = sampleManifest("server-pack", "pack_001", "pack_001");

    await storage.saveManifest({
      groupId: manifest.groupId,
      artifactKind: manifest.artifactKind,
      artifactId: manifest.artifactId,
      manifest,
    });

    assert.deepEqual(
      await storage.readManifest({
        groupId: manifest.groupId,
        artifactKind: manifest.artifactKind,
        artifactId: manifest.artifactId,
      }),
      manifest,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("saves and reads admin state manifests", async () => {
  const { storage, root } = await testStorage();
  try {
    const manifest = sampleManifest("admin-state", "admin_001", "pack_001");

    await storage.saveManifest({
      groupId: manifest.groupId,
      artifactKind: manifest.artifactKind,
      artifactId: manifest.artifactId,
      manifest,
    });

    assert.deepEqual(
      await storage.readManifest({
        groupId: manifest.groupId,
        artifactKind: manifest.artifactKind,
        artifactId: manifest.artifactId,
      }),
      manifest,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects unsafe artifact kinds, identifiers, and manifest paths", async () => {
  const { storage, root } = await testStorage();
  try {
    const content = Buffer.from("unsafe");
    await assert.rejects(
      storage.saveObject({
        groupId: "../outside",
        sha256: hash(content),
        content,
      }),
      StorageValidationError,
    );

    await assert.rejects(
      storage.readManifest({
        groupId: "grp_123",
        artifactKind: "../world-snapshot" as "world-snapshot",
        artifactId: "snap_001",
      }),
      StorageValidationError,
    );

    await assert.rejects(
      storage.readManifest({
        groupId: "grp_123",
        artifactKind: "world-snapshot",
        artifactId: "../snap_001",
      }),
      StorageValidationError,
    );

    const manifest = sampleManifest("world-snapshot", "snap_001", "pack_001", {
      files: [
        {
          path: "../server.properties",
          size: 1,
          sha256: hash(Buffer.from("x")),
          modifiedAt: "2026-06-05T00:00:00.000Z",
          deleted: false,
        },
      ],
    });

    await assert.rejects(
      storage.saveManifest({
        groupId: manifest.groupId,
        artifactKind: manifest.artifactKind,
        artifactId: manifest.artifactId,
        manifest,
      }),
      StorageValidationError,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("uses custom storage root from environment", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-custom-storage-"));
  try {
    const storage = createLocalFilesystemStorageFromEnv({
      ACBH_STORAGE_ROOT: root,
    });

    assert.equal(storage.info().root, path.resolve(root));
    assert.equal(storage.info().backend, "local");
    assert.equal(storage.info().ready, true);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("returns a clear not-found error for missing manifests", async () => {
  const { storage, root } = await testStorage();
  try {
    await assert.rejects(
      storage.readManifest({
        groupId: "grp_123",
        artifactKind: "world-snapshot",
        artifactId: "snap_missing",
      }),
      StorageNotFoundError,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

async function testStorage(): Promise<{ root: string; storage: LocalFilesystemStorage }> {
  const root = await mkdtemp(path.join(os.tmpdir(), "acbh-storage-"));
  return {
    root,
    storage: new LocalFilesystemStorage(root),
  };
}

function sampleManifest(
  artifactKind: ArtifactManifest["artifactKind"],
  artifactId: string,
  serverPackVersion: string,
  overrides: Partial<ArtifactManifest> = {},
): ArtifactManifest {
  const fileContent = Buffer.from("region data");

  return {
    artifactKind,
    artifactId,
    groupId: "grp_123",
    createdAt: "2026-06-05T00:00:00.000Z",
    creatorHostId: "host_123",
    parentArtifactId: null,
    serverPackVersion,
    files: [
      {
        path: "world/region/r.0.0.mca",
        size: fileContent.byteLength,
        sha256: hash(fileContent),
        modifiedAt: "2026-06-05T00:00:00.000Z",
        deleted: false,
        fileClass: artifactKind === "world-snapshot" ? "world-runtime" : artifactKind,
      },
    ],
    summary: {
      includedFiles: 1,
      ignoredFiles: 0,
      unknownFiles: 0,
      deletedFiles: 0,
      totalBytes: fileContent.byteLength,
    },
    ...overrides,
  };
}

function hash(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}
