import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

test("health endpoint reports package.json version", async () => {
  const packageJson = JSON.parse(
    await readFile(new URL("../package.json", import.meta.url), "utf8"),
  ) as { version: string };
  const app = await buildApp({ logger: false });
  try {
    const response = await app.inject({ method: "GET", url: "/health" });
    assert.equal(response.statusCode, 200);
    assert.equal(response.json<{ version: string }>().version, packageJson.version);
  } finally {
    await app.close();
  }
});