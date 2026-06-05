import { createHash } from "node:crypto";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import type { ArtifactKind } from "../domain/artifacts.js";
import {
  type ArtifactManifest,
  StorageNotFoundError,
  StorageValidationError,
  type CoordinatorStorage,
  type ObjectExistsParams,
  type ReadManifestParams,
  type ReadObjectParams,
  type SaveManifestParams,
  type SaveObjectParams,
  type StorageInfo,
} from "./types.js";
import {
  artifactKindDirectory,
  resolveUnderRoot,
  validateArtifactKind,
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
    await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
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
