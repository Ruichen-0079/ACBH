import type { Readable } from "node:stream";
import type { ArtifactKind, FileClass } from "../domain/artifacts.js";

export type SnapshotManifestFile = {
  path: string;
  size: number;
  sha256: string;
  modifiedAt: string;
  deleted: boolean;
  class?: FileClass;
  fileClass?: FileClass;
};

export type ArtifactManifestSummary = {
  includedFiles: number;
  ignoredFiles: number;
  unknownFiles: number;
  deletedFiles: number;
  totalBytes: number;
};

export type ArtifactManifest = {
  manifestVersion?: number;
  artifactKind: ArtifactKind;
  artifactId: string;
  groupId: string;
  createdAt: string;
  creatorHostId: string;
  generation?: number;
  parentArtifactId: string | null;
  serverPackVersion: string | null;
  files: SnapshotManifestFile[];
  summary?: ArtifactManifestSummary;
};

export type SaveObjectParams = {
  groupId: string;
  sha256: string;
  content: Uint8Array;
};

export type SaveObjectFromStreamParams = {
  groupId: string;
  sha256: string;
  stream: Readable;
  maxBytes: number;
};

export type ReadObjectParams = {
  groupId: string;
  sha256: string;
};

export type ObjectReadStream = {
  stream: Readable;
  size: number;
};

export type ObjectExistsParams = {
  groupId: string;
  sha256: string;
};

export type SaveManifestParams = {
  groupId: string;
  artifactKind: ArtifactKind;
  artifactId: string;
  manifest: ArtifactManifest;
  beforeCommit?: () => void;
};

export type ReadManifestParams = {
  groupId: string;
  artifactKind: ArtifactKind;
  artifactId: string;
};

export type StorageInfo = {
  backend: "local";
  root: string;
  ready: boolean;
};

export type DeleteObjectParams = {
  groupId: string;
  sha256: string;
};

export type DeleteManifestParams = {
  groupId: string;
  artifactKind: ArtifactKind;
  artifactId: string;
};

export type ListObjectsParams = {
  groupId: string;
};

export interface CoordinatorStorage {
  saveObject(params: SaveObjectParams): Promise<void>;
  saveObjectFromStream(params: SaveObjectFromStreamParams): Promise<{ size: number }>;
  readObject(params: ReadObjectParams): Promise<Buffer>;
  createObjectReadStream(params: ReadObjectParams): Promise<ObjectReadStream>;
  objectExists(params: ObjectExistsParams): Promise<boolean>;
  saveManifest(params: SaveManifestParams): Promise<void>;
  readManifest(params: ReadManifestParams): Promise<ArtifactManifest>;
  deleteObject(params: DeleteObjectParams): Promise<void>;
  deleteManifest(params: DeleteManifestParams): Promise<void>;
  listObjectSha256s(params: ListObjectsParams): Promise<string[]>;
  info(): StorageInfo;
}

export class StorageError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StorageError";
  }
}

export class StorageNotFoundError extends StorageError {
  constructor(message: string) {
    super(message);
    this.name = "StorageNotFoundError";
  }
}

export class StorageValidationError extends StorageError {
  constructor(message: string) {
    super(message);
    this.name = "StorageValidationError";
  }
}

export class StorageObjectTooLargeError extends StorageError {
  constructor(message: string) {
    super(message);
    this.name = "StorageObjectTooLargeError";
  }
}
