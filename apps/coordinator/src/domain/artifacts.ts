export type ArtifactKind = "server-pack" | "world-snapshot" | "server-runtime" | "admin-state";

export type FileClass =
  | "world-runtime"
  | "server-pack"
  | "server-runtime"
  | "admin-state"
  | "plugin-runtime-data"
  | "ignored"
  | "unknown";
