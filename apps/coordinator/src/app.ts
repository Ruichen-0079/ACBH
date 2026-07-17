import cors from "@fastify/cors";
import websocketPlugin from "@fastify/websocket";
import Fastify from "fastify";
import { registerDashboardRoutes } from "./dashboard.js";
import { registerRoutes } from "./routes.js";
import { createLocalFilesystemStorageFromEnv, type CoordinatorStorage } from "./storage/index.js";
import {
  createInMemoryCoordinatorStore,
  InMemoryCoordinatorStore,
} from "./store.js";
import { getStatePath, loadState, saveState } from "./persistence.js";
import { RelayManager } from "./relay.js";

export async function buildApp(options?: {
  store?: InMemoryCoordinatorStore;
  storage?: CoordinatorStorage;
  relay?: RelayManager;
  logger?: boolean;
  maxObjectBytes?: number;
  hobbyAccessToken?: string;
}) {
  const app = Fastify({ logger: options?.logger ?? true });

  let store: InMemoryCoordinatorStore;

  if (options?.store) {
    store = options.store;
  } else {
    const statePath = getStatePath();

    if (statePath) {
      let snapshot = null;
      try {
        snapshot = await loadState(statePath);
      } catch (err: unknown) {
        app.log.error(
          { err },
          `Failed to load coordinator state from ${statePath}, starting fresh`,
        );
      }

      if (snapshot) {
        app.log.info(
          `Loaded persisted coordinator state with ${snapshot.groups.length} group(s) from ${statePath}`,
        );
      } else {
        app.log.info(
          `No existing coordinator state file at ${statePath}, starting fresh`,
        );
      }

      let saveTimer: ReturnType<typeof setTimeout> | null = null;
      const debouncedSave = () => {
        if (saveTimer) clearTimeout(saveTimer);
        saveTimer = setTimeout(() => {
          saveState(statePath, store.snapshot()).catch((err: unknown) => {
            app.log.error({ err }, `Failed to persist coordinator state to ${statePath}`);
          });
        }, 100);
      };

      if (snapshot) {
        store = InMemoryCoordinatorStore.fromSnapshot(snapshot, { onMutation: debouncedSave });
      } else {
        store = createInMemoryCoordinatorStore({ onMutation: debouncedSave });
      }
    } else {
      app.log.info("Coordinator state persistence disabled");
      store = createInMemoryCoordinatorStore();
    }
  }

  const storage = options?.storage ?? createLocalFilesystemStorageFromEnv();

  const relay = options?.relay ?? new RelayManager(store);

  await app.register(cors, {
    origin: true,
  });

  await app.register(websocketPlugin);

  app.addContentTypeParser("application/octet-stream", (_request, payload, done) => {
    done(null, payload);
  });

  await registerDashboardRoutes(app);
  await registerRoutes(app, store, storage, relay, {
    maxObjectBytes: options?.maxObjectBytes,
    hobbyAccessToken: options?.hobbyAccessToken,
  });

  // Expose for public relay ingress and tests (v0.3.2)
  (app as any).store = store;
  (app as any).relay = relay;

  return app;
}
