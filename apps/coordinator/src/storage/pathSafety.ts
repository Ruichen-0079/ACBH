import path from "node:path";
import { StorageValidationError, type SnapshotManifest } from "./types.js";

const idPattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/;
const sha256Pattern = /^[a-f0-9]{64}$/;

export function validateStorageId(label: string, value: string): string {
  if (!idPattern.test(value) || value === "." || value === "..") {
    throw new StorageValidationError(`${label} is not a safe storage identifier`);
  }

  return value;
}

export function validateSha256(value: string): string {
  const normalized = value.toLowerCase();
  if (!sha256Pattern.test(normalized)) {
    throw new StorageValidationError("sha256 must be a 64-character lowercase hex digest");
  }

  return normalized;
}

export function validateManifest(manifest: SnapshotManifest): SnapshotManifest {
  validateStorageId("manifest snapshotId", manifest.snapshotId);
  validateStorageId("manifest groupId", manifest.groupId);
  validateStorageId("manifest serverPackVersion", manifest.serverPackVersion);
  validateStorageId("manifest creatorHostId", manifest.creatorHostId);
  if (manifest.parentSnapshotId !== null) {
    validateStorageId("manifest parentSnapshotId", manifest.parentSnapshotId);
  }
  if (Number.isNaN(Date.parse(manifest.createdAt))) {
    throw new StorageValidationError("manifest createdAt must be a valid timestamp");
  }
  if (!Array.isArray(manifest.files)) {
    throw new StorageValidationError("manifest files must be an array");
  }

  for (const file of manifest.files) {
    validateManifestPath(file.path);
    validateSha256(file.sha256);
    if (!Number.isSafeInteger(file.size) || file.size < 0) {
      throw new StorageValidationError("manifest file size must be a non-negative safe integer");
    }
    if (Number.isNaN(Date.parse(file.modifiedAt))) {
      throw new StorageValidationError("manifest file modifiedAt must be a valid timestamp");
    }
    if (typeof file.deleted !== "boolean") {
      throw new StorageValidationError("manifest file deleted must be a boolean");
    }
  }

  return manifest;
}

export function validateManifestPath(value: string): string {
  if (value.length === 0 || value.includes("\\") || path.posix.isAbsolute(value)) {
    throw new StorageValidationError("manifest file path must be a relative POSIX path");
  }

  const normalized = path.posix.normalize(value);
  if (normalized === "." || normalized.startsWith("../") || normalized.includes("/../")) {
    throw new StorageValidationError("manifest file path cannot traverse directories");
  }

  return normalized;
}

export function resolveUnderRoot(root: string, ...segments: string[]): string {
  const resolvedRoot = path.resolve(root);
  const target = path.resolve(resolvedRoot, ...segments);
  const relative = path.relative(resolvedRoot, target);

  if (relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative))) {
    return target;
  }

  throw new StorageValidationError("storage path escaped configured root");
}
