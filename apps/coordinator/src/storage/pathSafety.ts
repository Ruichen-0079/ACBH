import path from "node:path";
import type { ArtifactKind, FileClass } from "../domain/artifacts.js";
import { StorageValidationError, type ArtifactManifest } from "./types.js";

const idPattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/;
const sha256Pattern = /^[a-f0-9]{64}$/;
const artifactKinds = new Set<ArtifactKind>(["server-pack", "world-snapshot", "server-runtime", "admin-state"]);
const fileClasses = new Set<FileClass>([
  "world-runtime",
  "server-pack",
  "server-runtime",
  "admin-state",
  "plugin-runtime-data",
  "ignored",
  "unknown",
]);

const classAllowedForArtifact: Record<ArtifactKind, Set<FileClass>> = {
  "world-snapshot": new Set<FileClass>(["world-runtime", "plugin-runtime-data"]),
  "server-pack": new Set<FileClass>(["server-pack"]),
  "server-runtime": new Set<FileClass>(["server-runtime"]),
  "admin-state": new Set<FileClass>(["admin-state"]),
};

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
    case "server-runtime":
      return "server-runtimes";
    case "admin-state":
      return "admin-states";
  }
}

export function validateManifest(manifest: ArtifactManifest): ArtifactManifest {
  if (manifest.manifestVersion !== undefined && manifest.manifestVersion !== 1) {
    throw new StorageValidationError("manifest manifestVersion must be 1 when present");
  }
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
  if (
    manifest.generation !== undefined &&
    (!Number.isSafeInteger(manifest.generation) || manifest.generation < 0)
  ) {
    throw new StorageValidationError("manifest generation must be a non-negative safe integer");
  }
  if (manifest.artifactKind === "server-runtime" && manifest.generation === undefined) {
    throw new StorageValidationError("manifest generation is required for server-runtime artifacts");
  }
  if (!Array.isArray(manifest.files)) {
    throw new StorageValidationError("manifest files must be an array");
  }

  let seenPath = "";
  for (const file of manifest.files) {
    validateManifestPath(file.path);
    if (file.path <= seenPath) {
      throw new StorageValidationError("manifest files must be sorted by path with no duplicates");
    }
    seenPath = file.path;
    if (!Number.isSafeInteger(file.size) || file.size < 0) {
      throw new StorageValidationError("manifest file size must be a non-negative safe integer");
    }
    if (!file.deleted && Number.isNaN(Date.parse(file.modifiedAt))) {
      throw new StorageValidationError("manifest file modifiedAt must be a valid timestamp");
    }
    if (typeof file.deleted !== "boolean") {
      throw new StorageValidationError("manifest file deleted must be a boolean");
    }
    if (file.deleted) {
      if (file.size !== 0 || file.sha256 !== "") {
        throw new StorageValidationError("deleted manifest files must use size 0 and empty sha256");
      }
    } else {
      validateSha256(file.sha256);
    }
    if (file.fileClass !== undefined && file.class === undefined) {
      file.class = file.fileClass;
    }
    if (file.class === undefined) {
      throw new StorageValidationError("manifest file class is required");
    }
    if (!fileClasses.has(file.class)) {
      throw new StorageValidationError("manifest file class is not supported");
    }
    if (file.class === "ignored" || file.class === "unknown") {
      throw new StorageValidationError("manifest file class must not be ignored or unknown");
    }
    const allowed = classAllowedForArtifact[manifest.artifactKind];
    if (!allowed.has(file.class)) {
      throw new StorageValidationError(`manifest file class ${file.class} is not allowed for ${manifest.artifactKind}`);
    }
  }

  if (!manifest.summary) {
    throw new StorageValidationError("manifest summary is required");
  }
  {
    const included = (manifest.files ?? []).filter((f) => !f.deleted).length;
    const deleted = (manifest.files ?? []).filter((f) => f.deleted).length;
    if (manifest.summary.includedFiles !== included) {
      throw new StorageValidationError("manifest summary includedFiles must match file list");
    }
    if (manifest.summary.deletedFiles !== deleted) {
      throw new StorageValidationError("manifest summary deletedFiles must match file list");
    }
    if (manifest.summary.ignoredFiles < 0 || manifest.summary.unknownFiles < 0) {
      throw new StorageValidationError("manifest summary counts must be non-negative");
    }
    const totalBytes = (manifest.files ?? [])
      .filter((f) => !f.deleted)
      .reduce((sum, f) => sum + f.size, 0);
    if (manifest.summary.totalBytes !== totalBytes) {
      throw new StorageValidationError("manifest summary totalBytes must match file list");
    }
  }

  return manifest;
}

export function validateManifestPath(value: string): string {
  if (
    value.length === 0 ||
    value.includes("\0") ||
    value.includes("\\") ||
    /^[A-Za-z]:\//u.test(value) ||
    value.startsWith("//") ||
    path.posix.isAbsolute(value)
  ) {
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
