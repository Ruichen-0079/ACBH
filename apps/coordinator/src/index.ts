import cors from "@fastify/cors";
import Fastify from "fastify";
import { loadConfig } from "./config.js";

const config = loadConfig();
const app = Fastify({ logger: true });

await app.register(cors, {
  origin: true,
});

app.get("/health", async () => {
  return {
    ok: true,
    service: "acbh-coordinator",
    version: "0.1.0",
  };
});

app.get("/v1/info", async () => {
  return {
    name: "ACBH Coordinator",
    mode: "bootstrap",
    v1NonGoal: "hot-migration",
  };
});

try {
  await app.listen({ host: config.host, port: config.port });
} catch (error) {
  app.log.error(error);
  process.exit(1);
}
