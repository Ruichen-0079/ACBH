export type CoordinatorConfig = {
  host: string;
  port: number;
};

export function loadConfig(): CoordinatorConfig {
  const port = Number.parseInt(process.env.PORT ?? "6121", 10);

  if (!Number.isFinite(port) || port <= 0) {
    throw new Error("PORT must be a positive integer");
  }

  return {
    host: process.env.HOST ?? "0.0.0.0",
    port,
  };
}
