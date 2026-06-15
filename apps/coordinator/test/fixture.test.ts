import { randomUUID } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { test } from "node:test";
import { LocalFilesystemStorage, StorageValidationError } from "../src/storage/index.js";

function fixturePath(category: string, name: string): string {
  return fileURLToPath(new URL(`fixtures/manifests/${category}/${name}`, import.meta.url));
}

test("all valid shared fixtures pass TS validation", async () => {
  const validFixtures = ["world-snapshot.json", "server-pack.json", "admin-state.json", "with-tombstones.json"];

  for (const name of validFixtures) {
    const raw = await readFile(fixturePath("valid", name), "utf8");
    const manifest = JSON.parse(raw);

    assert.doesNotThrow(
      async () => {
        const root = await mkdtemp(path.join(os.tmpdir(), "acbh-fixture-"));
        const storage = new LocalFilesystemStorage(root);
        try {
          await storage.saveManifest({
            groupId: manifest.groupId,
            artifactKind: manifest.artifactKind,
            artifactId: manifest.artifactId,
            manifest,
          });
        } finally {
          await rm(root, { recursive: true, force: true });
        }
      },
      `valid fixture ${name} should pass validation`,
    );
  }
});

test("all invalid shared fixtures fail TS validation", async () => {
  const invalidCases = [
    { filename: "bad-version.json", msg: "manifestVersion" },
    { filename: "bad-kind.json", msg: "artifactKind" },
    { filename: "path-escape.json", msg: "path" },
    { filename: "bad-hash.json", msg: "sha256" },
    { filename: "bad-tombstone.json", msg: "deleted" },
    { filename: "missing-summary.json", msg: "summary" },
    { filename: "drive-relative.json", msg: "path" },
  ];

  for (const { filename, msg } of invalidCases) {
    const raw = await readFile(fixturePath("invalid", filename), "utf8");
    const manifest = JSON.parse(raw);

    await assert.rejects(
      async () => {
        const root = await mkdtemp(path.join(os.tmpdir(), "acbh-fixture-"));
        const storage = new LocalFilesystemStorage(root);
        try {
          await storage.saveManifest({
            groupId: manifest.groupId,
            artifactKind: manifest.artifactKind as any,
            artifactId: manifest.artifactId,
            manifest,
          });
        } finally {
          await rm(root, { recursive: true, force: true });
        }
      },
      (err: unknown) => {
        return err instanceof StorageValidationError &&
          err.message.toLowerCase().includes(msg.toLowerCase());
      },
      `invalid fixture ${filename} should fail with ${msg}`,
    );
  }
});
