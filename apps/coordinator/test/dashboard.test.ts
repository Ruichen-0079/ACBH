import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

test("dashboard route serves the web dashboard", async () => {
  const app = await buildApp({ logger: false });

  try {
    const response = await app.inject({
      method: "GET",
      url: "/dashboard",
    });

    assert.equal(response.statusCode, 200);
    assert.match(response.headers["content-type"] as string, /text\/html/);
    assert.match(response.body, /ACBH Dashboard/);
    assert.match(response.body, /Create group/);
    assert.match(response.body, /Command generator/);
    assert.match(response.body, /OpenCode handoff/);
    assert.match(response.body, /Host token/);
    assert.match(response.body, /External smoke test checklist/);
    assert.match(response.body, /Shell type/);
  } finally {
    await app.close();
  }
});
