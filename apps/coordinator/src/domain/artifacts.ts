export type ArtifactKind = "server-pack" | "world-snapshot" | "admin-state";

export type FileClass =
  | "world-runtime"
  | "server-pack"
  | "admin-state"
  | "plugin-runtime-data"
  | "ignored"
  | "unknown";
