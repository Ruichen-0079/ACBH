import { createHash, randomUUID } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, open, readdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { ArtifactKind } from "../domain/artifacts.js";
import {
  type ArtifactManifest,
  type DeleteManifestParams,
  type DeleteWorldSnapshotManifestParams,
  type DeleteObjectParams,
  type ListObjectsParams,
  type ListWorldSnapshotManifestParams,
  type ObjectReadStream,
  StorageObjectTooLargeError,
  StorageNotFoundError,
  StorageValidationError,
  type CoordinatorStorage,
  type ObjectExistsParams,
  type ReadWorldSnapshotManifestParams,
  type ReadManifestParams,
  type ReadObjectParams,
  type SaveManifestParams,
  type SaveObjectParams,
  type SaveObjectFromStreamParams,
  type SaveWorldSnapshotManifestParams,
  type StorageInfo,
  type WorldSnapshotManifest,
} from "./types.js";
import {
  artifactKindDirectory,
  resolveUnderRoot,
  validateArtifactKind,
  validateManifest,
  validateManifestPath,
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

  async saveObjectFromStream(params: SaveObjectFromStreamParams): Promise<{ size: number }> {
    const groupId = validateStorageId("groupId", params.groupId);
    const sha256 = validateSha256(params.sha256);
    if (!Number.isSafeInteger(params.maxBytes) || params.maxBytes < 0) {
      throw new StorageValidationError("maxBytes must be a non-negative safe integer");
    }

    const objectPath = this.objectPath(groupId, sha256);
    await mkdir(path.dirname(objectPath), { recursive: true });
    const temporaryPath = `${objectPath}.${randomUUID()}.tmp`;
    const verifier = new ObjectVerificationTransform(params.maxBytes);

    try {
      await pipeline(
        params.stream,
        verifier,
        createWriteStream(temporaryPath, { flags: "wx" }),
      );

      if (verifier.digest() !== sha256) {
        throw new StorageValidationError("object content does not match sha256");
      }

      await rename(temporaryPath, objectPath);
      return { size: verifier.size };
    } catch (error) {
      await rm(temporaryPath, { force: true });
      throw error;
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

  async createObjectReadStream(params: ReadObjectParams): Promise<ObjectReadStream> {
    const groupId = validateStorageId("groupId", params.groupId);
    const sha256 = validateSha256(params.sha256);
    const objectPath = this.objectPath(groupId, sha256);

    try {
      const info = await stat(objectPath);
      if (!info.isFile()) {
        throw new StorageNotFoundError("object does not exist");
      }
      return {
        stream: createReadStream(objectPath),
        size: info.size,
      };
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
    const artifactKind = validateArtifactKind(params.artifactKind);
    const artifactId = validateStorageId("artifactId", params.artifactId);
    const manifest = validateManifest(params.manifest);

    if (
      manifest.groupId !== groupId ||
      manifest.artifactKind !== artifactKind ||
      manifest.artifactId !== artifactId
    ) {
      throw new StorageValidationError("manifest IDs must match storage path IDs");
    }

    const manifestPath = this.manifestPath(groupId, artifactKind, artifactId);
    await mkdir(path.dirname(manifestPath), { recursive: true });
    const temporaryPath = `${manifestPath}.${randomUUID()}.tmp`;
    const content = `${JSON.stringify(manifest, null, 2)}\n`;

    try {
      const handle = await open(temporaryPath, "wx");
      try {
        await handle.writeFile(content, "utf8");
        await handle.sync();
      } finally {
        await handle.close();
      }

      params.beforeCommit?.();
      await rename(temporaryPath, manifestPath);
    } finally {
      await rm(temporaryPath, { force: true });
    }
  }

  async readManifest(params: ReadManifestParams): Promise<ArtifactManifest> {
    const groupId = validateStorageId("groupId", params.groupId);
    const artifactKind = validateArtifactKind(params.artifactKind);
    const artifactId = validateStorageId("artifactId", params.artifactId);
    const manifestPath = this.manifestPath(groupId, artifactKind, artifactId);

    let raw: string;
    try {
      raw = await readFile(manifestPath, "utf8");
    } catch (error) {
      if (isNotFound(error)) {
        throw new StorageNotFoundError("manifest does not exist");
      }
      throw error;
    }

    const parsed = JSON.parse(raw) as ArtifactManifest;
    return validateManifest(parsed);
  }

  async deleteObject(params: DeleteObjectParams): Promise<void> {
    const groupId = validateStorageId("groupId", params.groupId);
    const sha256 = validateSha256(params.sha256);
    const objectPath = this.objectPath(groupId, sha256);
    await rm(objectPath, { force: true });
  }

  async deleteManifest(params: DeleteManifestParams): Promise<void> {
    const groupId = validateStorageId("groupId", params.groupId);
    const artifactKind = validateArtifactKind(params.artifactKind);
    const artifactId = validateStorageId("artifactId", params.artifactId);
    const manifestPath = this.manifestPath(groupId, artifactKind, artifactId);
    await rm(manifestPath, { force: true });
  }

  async saveWorldSnapshotManifest(params: SaveWorldSnapshotManifestParams): Promise<void> {
    const groupId = validateStorageId("groupId", params.groupId);
    const snapshotId = validateStorageId("snapshotId", params.snapshotId);
    const manifest = validateWorldSnapshotManifest(params.manifest);
    if (manifest.groupId !== groupId || manifest.snapshotId !== snapshotId) {
      throw new StorageValidationError("world snapshot manifest IDs must match storage path IDs");
    }
    const manifestPath = this.worldSnapshotManifestPath(groupId, snapshotId);
    await mkdir(path.dirname(manifestPath), { recursive: true });
    const temporaryPath = `${manifestPath}.${randomUUID()}.tmp`;
    const content = `${JSON.stringify(manifest, null, 2)}\n`;
    try {
      const handle = await open(temporaryPath, "wx");
      try {
        await handle.writeFile(content, "utf8");
        await handle.sync();
      } finally {
        await handle.close();
      }
      params.beforeCommit?.();
      await rename(temporaryPath, manifestPath);
    } finally {
      await rm(temporaryPath, { force: true });
    }
  }

  async readWorldSnapshotManifest(params: ReadWorldSnapshotManifestParams): Promise<WorldSnapshotManifest> {
    const groupId = validateStorageId("groupId", params.groupId);
    const snapshotId = validateStorageId("snapshotId", params.snapshotId);
    const manifestPath = this.worldSnapshotManifestPath(groupId, snapshotId);
    let raw: string;
    try {
      raw = await readFile(manifestPath, "utf8");
    } catch (error) {
      if (isNotFound(error)) {
        throw new StorageNotFoundError("world snapshot manifest does not exist");
      }
      throw error;
    }
    return validateWorldSnapshotManifest(JSON.parse(raw) as WorldSnapshotManifest);
  }

  async deleteWorldSnapshotManifest(params: DeleteWorldSnapshotManifestParams): Promise<void> {
    const groupId = validateStorageId("groupId", params.groupId);
    const snapshotId = validateStorageId("snapshotId", params.snapshotId);
    await rm(this.worldSnapshotManifestPath(groupId, snapshotId), { force: true });
  }

  async listWorldSnapshotManifestIds(params: ListWorldSnapshotManifestParams): Promise<string[]> {
    const groupId = validateStorageId("groupId", params.groupId);
    const dir = resolveUnderRoot(this.root, "groups", groupId, "world-backups");
    try {
      const entries = await readdir(dir, { withFileTypes: true });
      return entries.filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort();
    } catch (error) {
      if (isNotFound(error)) {
        return [];
      }
      throw error;
    }
  }

  async listObjectSha256s(params: ListObjectsParams): Promise<string[]> {
    const groupId = validateStorageId("groupId", params.groupId);
    const objectsDir = resolveUnderRoot(this.root, "groups", groupId, "objects", "sha256");
    const sha256s: string[] = [];
    try {
      const prefixDirs = await readdir(objectsDir, { withFileTypes: true });
      for (const prefix of prefixDirs) {
        if (!prefix.isDirectory() || prefix.name.length !== 2) continue;
        const prefixDir = path.join(objectsDir, prefix.name);
        const files = await readdir(prefixDir, { withFileTypes: true });
        for (const file of files) {
          if (file.isFile() && /^[a-f0-9]{64}$/.test(file.name)) {
            sha256s.push(file.name);
          }
        }
      }
    } catch (error) {
      if (isNotFound(error)) {
        return [];
      }
      throw error;
    }
    return sha256s;
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

  private manifestPath(groupId: string, artifactKind: ArtifactKind, artifactId: string): string {
    return resolveUnderRoot(
      this.root,
      "groups",
      groupId,
      artifactKindDirectory(artifactKind),
      artifactId,
      "manifest.json",
    );
  }

  private worldSnapshotManifestPath(groupId: string, snapshotId: string): string {
    return resolveUnderRoot(this.root, "groups", groupId, "world-backups", snapshotId, "manifest.json");
  }
}

class ObjectVerificationTransform extends Transform {
  readonly hash = createHash("sha256");
  size = 0;

  constructor(private readonly maxBytes: number) {
    super();
  }

  override _transform(
    chunk: Buffer,
    encoding: BufferEncoding,
    callback: (error?: Error | null, data?: Buffer) => void,
  ): void {
    const content = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk, encoding);
    this.size += content.byteLength;
    if (this.size > this.maxBytes) {
      callback(
        new StorageObjectTooLargeError(
          `Object upload exceeds configured limit of ${this.maxBytes} bytes`,
        ),
      );
      return;
    }

    this.hash.update(content);
    callback(null, content);
  }

  digest(): string {
    return this.hash.digest("hex");
  }
}

export function createLocalFilesystemStorageFromEnv(env = process.env): LocalFilesystemStorage {
  return new LocalFilesystemStorage(env[storageRootEnvVar] ?? defaultStorageRoot);
}

function hashContent(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}

function validateWorldSnapshotManifest(manifest: WorldSnapshotManifest): WorldSnapshotManifest {
  if (manifest.schemaVersion !== 1) {
    throw new StorageValidationError("world snapshot schemaVersion must be 1");
  }
  validateStorageId("snapshotId", manifest.snapshotId);
  validateStorageId("groupId", manifest.groupId);
  validateStorageId("sourceHostId", manifest.sourceHostId);
  if (manifest.parentSnapshotId !== undefined && manifest.parentSnapshotId !== "") {
    validateStorageId("parentSnapshotId", manifest.parentSnapshotId);
  }
  if (!Number.isSafeInteger(manifest.hostGeneration) || manifest.hostGeneration < 0) {
    throw new StorageValidationError("hostGeneration must be a non-negative safe integer");
  }
  if (Number.isNaN(Date.parse(manifest.createdAt))) {
    throw new StorageValidationError("createdAt must be a valid timestamp");
  }
  if (typeof manifest.consistent !== "boolean") {
    throw new StorageValidationError("consistent must be a boolean");
  }
  for (const [label, value] of [
    ["logicalSize", manifest.logicalSize],
    ["uploadedSize", manifest.uploadedSize],
    ["fileCount", manifest.fileCount],
    ["changedFileCount", manifest.changedFileCount],
    ["deletedFileCount", manifest.deletedFileCount],
  ] as const) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new StorageValidationError(`${label} must be a non-negative safe integer`);
    }
  }
  if (!Array.isArray(manifest.files)) {
    throw new StorageValidationError("files must be an array");
  }
  if (manifest.fileCount !== manifest.files.length) {
    throw new StorageValidationError("fileCount must match files length");
  }
  let lastPath = "";
  let logicalSize = 0;
  for (const file of manifest.files) {
    validateManifestPath(file.path);
    if (file.path <= lastPath) {
      throw new StorageValidationError("files must be sorted by path with no duplicates");
    }
    lastPath = file.path;
    if (!Number.isSafeInteger(file.size) || file.size < 0) {
      throw new StorageValidationError("world snapshot file size must be a non-negative safe integer");
    }
    const sha = validateSha256(file.sha256);
    if (file.objectId !== `sha256:${sha}`) {
      throw new StorageValidationError("world snapshot objectId must match sha256");
    }
    logicalSize += file.size;
  }
  if (manifest.logicalSize !== logicalSize) {
    throw new StorageValidationError("logicalSize must match files total size");
  }
  const deletedPaths = manifest.deletedPaths ?? [];
  if (!Array.isArray(deletedPaths)) {
    throw new StorageValidationError("deletedPaths must be an array");
  }
  if (manifest.deletedFileCount !== deletedPaths.length) {
    throw new StorageValidationError("deletedFileCount must match deletedPaths length");
  }
  let lastDeleted = "";
  for (const deletedPath of deletedPaths) {
    validateManifestPath(deletedPath);
    if (deletedPath <= lastDeleted) {
      throw new StorageValidationError("deletedPaths must be sorted by path with no duplicates");
    }
    lastDeleted = deletedPath;
  }
  return manifest;
}

function isNotFound(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}
