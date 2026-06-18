export type CoordinatorConfig = {
  host: string;
  port: number;
  relayPublicHost: string;
  relayPublicPort: number;
};

export function loadConfig(): CoordinatorConfig {
  const port = Number.parseInt(process.env.PORT ?? "6121", 10);

  if (!Number.isFinite(port) || port <= 0) {
    throw new Error("PORT must be a positive integer");
  }

  const relayPublicPort = Number.parseInt(process.env.ACBH_RELAY_PUBLIC_PORT ?? "0", 10);

  return {
    host: process.env.HOST ?? "0.0.0.0",
    port,
    relayPublicHost: process.env.ACBH_RELAY_PUBLIC_HOST ?? "0.0.0.0",
    relayPublicPort: Number.isFinite(relayPublicPort) ? relayPublicPort : 0,
  };
}
