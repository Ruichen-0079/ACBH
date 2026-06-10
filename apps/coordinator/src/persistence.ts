import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

import type { StoreSnapshot } from "./store.js";

export type PersistedCoordinatorState = {
  version: 1;
  savedAt: string;
  state: StoreSnapshot;
};

const CURRENT_VERSION = 1;

export function getStatePath(): string | null {
  const raw = process.env.ACBH_COORDINATOR_STATE_PATH;
  if (raw === undefined || raw.trim() === "") {
    return null;
  }
  return raw.trim();
}

export async function loadState(statePath: string): Promise<StoreSnapshot | null> {
  let raw: string;
  try {
    raw = await readFile(statePath, "utf8");
  } catch (err: unknown) {
    if (isNodeError(err) && err.code === "ENOENT") {
      return null;
    }
    throw err;
  }

  let parsed: PersistedCoordinatorState;
  try {
    parsed = JSON.parse(raw) as PersistedCoordinatorState;
  } catch {
    throw new Error("Coordinator state file is corrupt (invalid JSON)");
  }

  if (parsed.version !== CURRENT_VERSION) {
    throw new Error(
      `Unknown coordinator state version ${String(parsed.version)} (expected ${CURRENT_VERSION})`,
    );
  }

  const state = parsed.state;

  if (!Array.isArray(state?.groups)) {
    throw new Error("Coordinator state file is corrupt (missing state.groups)");
  }

  return state;
}

export async function saveState(statePath: string, snapshot: StoreSnapshot): Promise<void> {
  const data: PersistedCoordinatorState = {
    version: CURRENT_VERSION,
    savedAt: new Date().toISOString(),
    state: snapshot,
  };

  const tmpPath = statePath + ".tmp";

  await mkdir(dirname(statePath), { recursive: true });
  await writeFile(tmpPath, JSON.stringify(data, null, 2), "utf8");
  await rename(tmpPath, statePath);
}

function isNodeError(err: unknown): err is NodeJS.ErrnoException {
  return err instanceof Error && "code" in err;
}
