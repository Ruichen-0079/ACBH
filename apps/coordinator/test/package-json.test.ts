import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

test("runtime websocket package is installed as a production dependency", async () => {
  const packageJson = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8")) as {
    dependencies?: Record<string, string>;
    devDependencies?: Record<string, string>;
  };

  assert.ok(packageJson.dependencies?.ws, "ws must be present in dependencies for production bundles");
  assert.equal(packageJson.devDependencies?.ws, undefined, "ws must not be only a devDependency");
});
