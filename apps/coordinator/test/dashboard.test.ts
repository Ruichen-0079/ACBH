import assert from "node:assert/strict";
import { test } from "node:test";
import vm from "node:vm";
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
    assert.match(response.body, /\u670d\u52a1\u5668\u63a7\u5236/u);
    assert.match(response.body, /\u542f\u52a8\u670d\u52a1\u5668/u);
    assert.match(response.body, /\u505c\u6b62\u670d\u52a1\u5668/u);
    assert.match(response.body, /\u67e5\u770b\u72b6\u6001/u);
    assert.match(response.body, /\u672c\u5730\u63a7\u5236\u6a21\u5f0f/u);
    assert.match(response.body, /\u547d\u4ee4\u6a21\u5f0f/u);
    assert.match(response.body, /\u670d\u52a1\u7aef\u76ee\u5f55/u);
    assert.match(response.body, /\u542f\u52a8 Jar/u);
    assert.match(response.body, /JVM \u53c2\u6570/u);
    assert.match(response.body, /RCON \u5bc6\u7801/u);
    assert.match(response.body, /var persistedKeys=/);
    assert.match(response.body, /var secretKeys=/);
    assert.match(response.body, /persistedKeys=\["coordinatorUrl","groupId","memberId","hostId"/);
    assert.doesNotMatch(response.body, /persistedKeys=\[[^\]]*"accessKey"/);
    assert.doesNotMatch(response.body, /persistedKeys=\[[^\]]*"hostToken"/);
    assert.doesNotMatch(response.body, /persistedKeys=\[[^\]]*"agentToken"/);
    assert.match(response.body, /localStorage\.removeItem\("acbh\."\+secretKeys/);
    assert.match(response.body, /localStorage\.setItem\("acbh\."\+k,el\.value\)/);
    assert.match(response.body, /id="accessKey" type="password"/);
    assert.match(response.body, /id="hostToken" type="password"/);
    assert.match(response.body, /id="agentToken" type="password"/);
    assert.match(response.body, /function forgetSecrets\(\)/);
    assert.match(response.body, /local_control_auth_failed/);
    assert.match(response.body, /Register host/);
    assert.match(response.body, /Send heartbeat/);
    assert.match(response.body, /Validate manifest/);
    assert.match(response.body, /Election \/ Takeover/);
    assert.match(response.body, /Events \/ Logs/);
    assert.match(response.body, /function checkLocalControlUrl\(\)/);
    assert.match(response.body, /This Local Control URL is not loopback/);
    assert.match(response.body, /function redact\(v\)/);
    assert.match(response.body, /if\(r\.status===401\|\|r\.status===403\)/);
    assert.match(response.body, /\$\("agentToken"\)\.value=""/);
    assert.match(response.body, /confirm\("Pull and restore/);
    assert.match(response.body, /confirm\("Stop the managed Minecraft server/);
    assert.match(response.body, /id="takeoverToken" type="password"/);
    assert.match(response.body, /secretEnvSetup\("ACBH_ACCESS_KEY"/);
    assert.match(response.body, /secretEnvSetup\("ACBH_RCON_PASSWORD"/);
    assert.doesNotMatch(response.body, /"--access-key "\+\$\("accessKey"\)\.value/);
    assert.doesNotMatch(response.body, /"--rcon-password "\+rpw/);
    assert.doesNotMatch(response.body, /sessionStorage\./);
    assert.doesNotMatch(response.body, /console\.log/);

    const inlineScript = response.body.match(/<script>([\s\S]*?)<\/script>/u);
    assert.ok(inlineScript, "dashboard inline script must exist");
    assert.doesNotThrow(() => new vm.Script(inlineScript[1], { filename: "dashboard-inline.js" }));
  } finally {
    await app.close();
  }
});
