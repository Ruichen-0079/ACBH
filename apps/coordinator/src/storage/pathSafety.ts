import path from "node:path";
import type { ArtifactKind, FileClass } from "../domain/artifacts.js";
import { StorageValidationError, type ArtifactManifest } from "./types.js";

const idPattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/;
const sha256Pattern = /^[a-f0-9]{64}$/;
const artifactKinds = new Set<ArtifactKind>(["server-pack", "world-snapshot", "admin-state"]);
const fileClasses = new Set<FileClass>([
  "world-runtime",
  "server-pack",
  "admin-state",
  "plugin-runtime-data",
  "ignored",
  "unknown",
]);

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

export function validateArtifactKind(value: string): ArtifactKind {
  if (!artifactKinds.has(value as ArtifactKind)) {
    throw new StorageValidationError("artifactKind is not supported");
  }

  return value as ArtifactKind;
}

export function artifactKindDirectory(kind: ArtifactKind): string {
  switch (kind) {
    case "server-pack":
      return "server-packs";
    case "world-snapshot":
      return "world-snapshots";
    case "admin-state":
      return "admin-states";
  }
}

export function validateManifest(manifest: ArtifactManifest): ArtifactManifest {
  validateArtifactKind(manifest.artifactKind);
  validateStorageId("manifest artifactId", manifest.artifactId);
  validateStorageId("manifest groupId", manifest.groupId);
  validateStorageId("manifest creatorHostId", manifest.creatorHostId);
  if (manifest.parentArtifactId !== null) {
    validateStorageId("manifest parentArtifactId", manifest.parentArtifactId);
  }
  if (manifest.serverPackVersion !== null) {
    validateStorageId("manifest serverPackVersion", manifest.serverPackVersion);
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
    if (file.fileClass !== undefined && !fileClasses.has(file.fileClass)) {
      throw new StorageValidationError("manifest file fileClass is not supported");
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
