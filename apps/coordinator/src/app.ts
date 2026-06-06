import cors from "@fastify/cors";
import Fastify from "fastify";
import { registerRoutes } from "./routes.js";
import { createLocalFilesystemStorageFromEnv, type CoordinatorStorage } from "./storage/index.js";
import {
  createInMemoryCoordinatorStore,
  type InMemoryCoordinatorStore,
} from "./store.js";

export async function buildApp(options?: {
  store?: InMemoryCoordinatorStore;
  storage?: CoordinatorStorage;
  logger?: boolean;
  maxObjectBytes?: number;
}) {
  const app = Fastify({ logger: options?.logger ?? true });
  const store = options?.store ?? createInMemoryCoordinatorStore();
  const storage = options?.storage ?? createLocalFilesystemStorageFromEnv();

  await app.register(cors, {
    origin: true,
  });

  app.addContentTypeParser("application/octet-stream", (_request, payload, done) => {
    done(null, payload);
  });

  await registerRoutes(app, store, storage, {
    maxObjectBytes: options?.maxObjectBytes,
  });

  return app;
}
