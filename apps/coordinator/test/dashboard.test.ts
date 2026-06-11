import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

test("dashboard route serves the Chinese control center with Agent local control", async () => {
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
    assert.match(response.body, /Agent 本地控制/u);
    assert.match(response.body, /本地控制模式/u);
    assert.match(response.body, /Agent 本地 API 地址/u);
    assert.match(response.body, /本地控制令牌/u);
    assert.match(response.body, /运行 doctor/u);
    assert.match(response.body, /扫描 server-pack/u);
    assert.match(response.body, /safe-sync/u);
    assert.match(response.body, /push/u);
    assert.match(response.body, /pull/u);
  } finally {
    await app.close();
  }
});
