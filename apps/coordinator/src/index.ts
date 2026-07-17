import { buildApp } from "./app.js";
import { loadConfig } from "./config.js";
import { PublicRelayIngress } from "./public-relay.js";
import { createInMemoryCoordinatorStore } from "./store.js";
import { RelayManager } from "./relay.js";

const config = loadConfig();
const app = await buildApp();

try {
  await app.listen({ host: config.host, port: config.port });
  app.log.info(`Coordinator API listening on ${config.host}:${config.port}`);

  // Start public relay ingress if configured
  if (config.relayPublicPort > 0) {
    const store = (app as any).store;
    const relay = (app as any).relay;
    const ingress = new PublicRelayIngress({
      host: config.relayPublicHost,
      port: config.relayPublicPort,
      coordinatorBaseURL: `http://127.0.0.1:${config.port}`,
      store,
      relay,
      logger: (msg, meta) => app.log.info({ ...meta }, msg),
    });
    ingress.start();
    (app as any).publicRelay = ingress;
  }
} catch (error) {
  app.log.error(error);
  process.exit(1);
}
