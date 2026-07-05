import { buildApp } from "./app.js";
import { loadConfig } from "./config.js";

const config = loadConfig();
const app = await buildApp();

try {
  await app.listen({ host: config.host, port: config.port });
  app.log.info(`Coordinator API listening on ${config.host}:${config.port}`);

  // Start public relay ingress if configured
  if (config.relayPublicPort > 0) {
    const ingress = (app as any).publicRelay;
    await ingress.start();
  }
} catch (error) {
  app.log.error(error);
  process.exit(1);
}
