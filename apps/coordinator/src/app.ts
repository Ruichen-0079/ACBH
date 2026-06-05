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
}) {
  const app = Fastify({ logger: options?.logger ?? true });
  const store = options?.store ?? createInMemoryCoordinatorStore();
  const storage = options?.storage ?? createLocalFilesystemStorageFromEnv();

  await app.register(cors, {
    origin: true,
  });

  await registerRoutes(app, store, storage);

  return app;
}
