import cors from "@fastify/cors";
import Fastify from "fastify";
import { registerRoutes } from "./routes.js";
import {
  createInMemoryCoordinatorStore,
  type InMemoryCoordinatorStore,
} from "./store.js";

export async function buildApp(options?: {
  store?: InMemoryCoordinatorStore;
  logger?: boolean;
}) {
  const app = Fastify({ logger: options?.logger ?? true });
  const store = options?.store ?? createInMemoryCoordinatorStore();

  await app.register(cors, {
    origin: true,
  });

  await registerRoutes(app, store);

  return app;
}
