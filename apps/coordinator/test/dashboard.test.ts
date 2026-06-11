import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

test("dashboard route serves the Chinese control center", async () => {
  const app = await buildApp({ logger: false });

  try {
    const response = await app.inject({
      method: "GET",
      url: "/dashboard",
    });

    assert.equal(response.statusCode, 200);
    assert.match(response.headers["content-type"] as string, /text\/html/);
    assert.match(response.body, /ACBH/u);
    assert.match(response.body, /控制中心/u);
    assert.match(response.body, /总览/u);
    assert.match(response.body, /Coordinator/);
    assert.match(response.body, /Agent/);
    assert.match(response.body, /Storage/);
    assert.match(response.body, /OpenCode/);
    assert.match(response.body, /检查清单/u);
    assert.match(response.body, /看板助手/u);
    assert.match(response.body, /主机令牌/u);
    assert.match(response.body, /命令模式/u);
    assert.match(response.body, /制品/u);
  } finally {
    await app.close();
  }
});
