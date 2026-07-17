import assert from "node:assert/strict";
import { test } from "node:test";
import { buildApp } from "../src/app.js";

const accessToken = "hobby-test-secret";

function heartbeat(protocolVersion = 1) {
  return {
    protocol_version: protocolVersion,
    node_id: "node-1",
    node_name: "Gaming PC",
    agent_version: "0.4.0-hobby",
    minecraft: { state: "READY" },
    relay: { state: "ONLINE" },
    overall: { state: "ONLINE" },
	minecraft_local_port: 25566,
	public_minecraft_port: 25575,
	public_endpoint: "vps.example.test:25575",
  };
}

test("Hobby info exposes FRP control values without player relay routes", async (t) => {
  const app = await buildApp({ logger: false, hobbyAccessToken: accessToken });
  t.after(() => app.close());

  const response = await app.inject({ method: "GET", url: "/v1/info" });
  assert.equal(response.statusCode, 200);
  assert.deepEqual(
    {
      protocol_version: response.json().protocol_version,
      frp_server_port: response.json().frp_server_port,
      public_minecraft_port: response.json().public_minecraft_port,
      heartbeat_interval_seconds: response.json().heartbeat_interval_seconds,
    },
    {
      protocol_version: 1,
      frp_server_port: 7000,
      public_minecraft_port: 25565,
      heartbeat_interval_seconds: 10,
    },
  );
});

test("Hobby heartbeat authenticates and upserts a node", async (t) => {
  const app = await buildApp({ logger: false, hobbyAccessToken: accessToken });
  t.after(() => app.close());

  const denied = await app.inject({ method: "POST", url: "/v1/heartbeat", payload: heartbeat() });
  assert.equal(denied.statusCode, 401);

  const accepted = await app.inject({
    method: "POST",
    url: "/v1/heartbeat",
    headers: { authorization: `Bearer ${accessToken}` },
    payload: heartbeat(),
    remoteAddress: "203.0.113.42:43123",
  });
  assert.equal(accepted.statusCode, 200);
  assert.equal(accepted.json().state, "ONLINE");

  const nodes = await app.inject({
    method: "GET",
    url: "/v1/nodes",
    headers: { authorization: `Bearer ${accessToken}` },
  });
  assert.equal(nodes.statusCode, 200);
  assert.equal(nodes.json().nodes.length, 1);
  assert.equal(nodes.json().nodes[0].node_id, "node-1");
	assert.equal(nodes.json().nodes[0].minecraft_local_port, 25566);
	assert.equal(nodes.json().nodes[0].public_minecraft_port, 25575);
	assert.equal(nodes.json().nodes[0].public_endpoint, "vps.example.test:25575");
  assert.equal("remote_address" in nodes.json().nodes[0], false);
  assert.equal(nodes.body.includes("203.0.113.42"), false);
  assert.equal(nodes.body.includes("43123"), false);
  assert.equal(nodes.body.includes(accessToken), false);
});

test("Hobby heartbeat rejects incompatible protocol without leaking token", async (t) => {
  const app = await buildApp({ logger: false, hobbyAccessToken: accessToken });
  t.after(() => app.close());

  const response = await app.inject({
    method: "POST",
    url: "/v1/heartbeat",
    headers: { authorization: `Bearer ${accessToken}` },
    payload: heartbeat(2),
  });
  assert.equal(response.statusCode, 409);
  assert.equal(response.json().code, "protocol_incompatible");
  assert.equal(response.body.includes(accessToken), false);
});
