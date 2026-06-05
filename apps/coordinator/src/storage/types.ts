export type SnapshotManifestFile = {
  path: string;
  size: number;
  sha256: string;
  modifiedAt: string;
  deleted: boolean;
};

export type SnapshotManifest = {
  snapshotId: string;
  groupId: string;
  serverPackVersion: string;
  parentSnapshotId: string | null;
  createdAt: string;
  creatorHostId: string;
  files: SnapshotManifestFile[];
};

export type SaveObjectParams = {
  groupId: string;
  sha256: string;
  content: Uint8Array;
};

export type ReadObjectParams = {
  groupId: string;
  sha256: string;
};

export type ObjectExistsParams = {
  groupId: string;
  sha256: string;
};

export type SaveManifestParams = {
  groupId: string;
  snapshotId: string;
  manifest: SnapshotManifest;
};

export type ReadManifestParams = {
  groupId: string;
  snapshotId: string;
};

export type StorageInfo = {
  backend: "local";
  root: string;
  ready: boolean;
};

export interface CoordinatorStorage {
  saveObject(params: SaveObjectParams): Promise<void>;
  readObject(params: ReadObjectParams): Promise<Buffer>;
  objectExists(params: ObjectExistsParams): Promise<boolean>;
  saveManifest(params: SaveManifestParams): Promise<void>;
  readManifest(params: ReadManifestParams): Promise<SnapshotManifest>;
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
