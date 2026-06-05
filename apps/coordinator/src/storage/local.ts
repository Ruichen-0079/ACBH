import { createHash } from "node:crypto";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import {
  StorageNotFoundError,
  StorageValidationError,
  type CoordinatorStorage,
  type ObjectExistsParams,
  type ReadManifestParams,
  type ReadObjectParams,
  type SaveManifestParams,
  type SaveObjectParams,
  type SnapshotManifest,
  type StorageInfo,
} from "./types.js";
import {
  resolveUnderRoot,
  validateManifest,
  validateSha256,
  validateStorageId,
} from "./pathSafety.js";

export const defaultStorageRoot = ".acbh-storage";
export const storageRootEnvVar = "ACBH_STORAGE_ROOT";

export class LocalFilesystemStorage implements CoordinatorStorage {
  private readonly root: string;

  constructor(root = defaultStorageRoot) {
    this.root = path.resolve(root);
  }

  async saveObject(params: SaveObjectParams): Promise<void> {
    const groupId = validateStorageId("groupId", params.groupId);
    const sha256 = validateSha256(params.sha256);
    const content = Buffer.from(params.content);
    const actual = hashContent(content);

    if (actual !== sha256) {
      throw new StorageValidationError("object content does not match sha256");
    }

    const objectPath = this.objectPath(groupId, sha256);
    await mkdir(path.dirname(objectPath), { recursive: true });
    await writeFile(objectPath, content);

    const written = await readFile(objectPath);
    if (hashContent(written) !== sha256) {
      throw new StorageValidationError("written object failed sha256 verification");
    }
  }

  async readObject(params: ReadObjectParams): Promise<Buffer> {
    const groupId = validateStorageId("groupId", params.groupId);
    const sha256 = validateSha256(params.sha256);
    const objectPath = this.objectPath(groupId, sha256);

    try {
      return await readFile(objectPath);
    } catch (error) {
      if (isNotFound(error)) {
        throw new StorageNotFoundError("object does not exist");
      }
      throw error;
    }
  }

  async objectExists(params: ObjectExistsParams): Promise<boolean> {
    const groupId = validateStorageId("groupId", params.groupId);
    const sha256 = validateSha256(params.sha256);

    try {
      const info = await stat(this.objectPath(groupId, sha256));
      return info.isFile();
    } catch (error) {
      if (isNotFound(error)) {
        return false;
      }
      throw error;
    }
  }

  async saveManifest(params: SaveManifestParams): Promise<void> {
    const groupId = validateStorageId("groupId", params.groupId);
    const snapshotId = validateStorageId("snapshotId", params.snapshotId);
    const manifest = validateManifest(params.manifest);

    if (manifest.groupId !== groupId || manifest.snapshotId !== snapshotId) {
      throw new StorageValidationError("manifest IDs must match storage path IDs");
    }

    const manifestPath = this.manifestPath(groupId, snapshotId);
    await mkdir(path.dirname(manifestPath), { recursive: true });
    await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
  }

  async readManifest(params: ReadManifestParams): Promise<SnapshotManifest> {
    const groupId = validateStorageId("groupId", params.groupId);
    const snapshotId = validateStorageId("snapshotId", params.snapshotId);
    const manifestPath = this.manifestPath(groupId, snapshotId);

    let raw: string;
    try {
      raw = await readFile(manifestPath, "utf8");
    } catch (error) {
      if (isNotFound(error)) {
        throw new StorageNotFoundError("manifest does not exist");
      }
      throw error;
    }

    const parsed = JSON.parse(raw) as SnapshotManifest;
    return validateManifest(parsed);
  }

  info(): StorageInfo {
    return {
      backend: "local",
      root: this.root,
      ready: true,
    };
  }

  private objectPath(groupId: string, sha256: string): string {
    return resolveUnderRoot(
      this.root,
      "groups",
      groupId,
      "objects",
      "sha256",
      sha256.slice(0, 2),
      sha256,
    );
  }

  private manifestPath(groupId: string, snapshotId: string): string {
    return resolveUnderRoot(this.root, "groups", groupId, "snapshots", snapshotId, "manifest.json");
  }
}

export function createLocalFilesystemStorageFromEnv(env = process.env): LocalFilesystemStorage {
  return new LocalFilesystemStorage(env[storageRootEnvVar] ?? defaultStorageRoot);
}

function hashContent(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}

function isNotFound(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}
