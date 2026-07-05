import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const packageJsonPath = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "package.json");

export function coordinatorVersion(): string {
  const raw = JSON.parse(readFileSync(packageJsonPath, "utf8")) as { version?: string };
  if (!raw.version) {
    return "v0.0.0";
  }
  return raw.version.startsWith("v") ? raw.version : `v${raw.version}`;
}
