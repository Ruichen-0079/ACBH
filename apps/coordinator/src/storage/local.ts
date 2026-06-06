import { createHash, randomUUID } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { ArtifactKind } from "../domain/artifacts.js";
import {
  type ArtifactManifest,
  type ObjectReadStream,
  StorageObjectTooLargeError,
  StorageNotFoundError,
  StorageValidationError,
  type CoordinatorStorage,
  type ObjectExistsParams,
  type ReadManifestParams,
  type ReadObjectParams,
  type SaveManifestParams,
  type SaveObjectParams,
  type SaveObjectFromStreamParams,
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

function isNotFound(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}
