import type { FastifyInstance, FastifyReply } from "fastify";

const dashboardHtml = String.raw`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>ACBH Dashboard</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #0b1020; color: #e5e7eb; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: radial-gradient(circle at top left, #1f2a44 0, #0b1020 42rem); }
    header { padding: 32px 28px 12px; }
    h1 { margin: 0; font-size: 32px; letter-spacing: -0.04em; }
    h2 { margin: 0 0 14px; font-size: 18px; }
    p { color: #9ca3af; }
    main { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 16px; padding: 16px 28px 32px; }
    section { border: 1px solid rgba(148, 163, 184, 0.22); background: rgba(15, 23, 42, 0.76); border-radius: 18px; padding: 18px; box-shadow: 0 16px 45px rgba(0,0,0,.24); }
    label { display: block; margin: 10px 0 6px; color: #cbd5e1; font-size: 13px; }
    input, select, textarea, button { width: 100%; border-radius: 12px; border: 1px solid rgba(148,163,184,.28); background: #111827; color: #f9fafb; padding: 10px 12px; font: inherit; }
    textarea { min-height: 110px; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; }
    button { margin-top: 12px; background: linear-gradient(135deg, #2563eb, #7c3aed); border: 0; cursor: pointer; font-weight: 700; }
    button.secondary { background: #1f2937; border: 1px solid rgba(148,163,184,.28); }
    pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #020617; border: 1px solid rgba(148,163,184,.18); border-radius: 12px; padding: 12px; color: #d1d5db; font-size: 12px; max-height: 360px; overflow: auto; }
    .row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .pill { display: inline-flex; align-items: center; gap: 8px; padding: 6px 10px; border-radius: 999px; background: rgba(34,197,94,.12); color: #86efac; font-size: 13px; }
    .muted { color: #94a3b8; font-size: 13px; }
    .full { grid-column: 1 / -1; }
  </style>
</head>
<body>
  <header>
    <div class="pill" id="health">Checking coordinator...</div>
    <h1>ACBH Dashboard</h1>
    <p>Quick setup, group state, artifact visibility, and copy-paste Agent commands.</p>
  </header>
  <main>
    <section>
      <h2>Coordinator</h2>
      <label>Coordinator URL</label>
      <input id="coordinatorUrl" />
      <button onclick="refreshHealth()">Refresh health</button>
      <button class="secondary" onclick="refreshStorage()">Refresh storage</button>
      <pre id="healthOut"></pre>
    </section>

    <section>
      <h2>Create group</h2>
      <label>Group name</label>
      <input id="groupName" value="Local Test" />
      <label>Owner name</label>
      <input id="ownerName" value="Owner" />
      <button onclick="createGroup()">Create group</button>
      <pre id="groupOut"></pre>
    </section>

    <section>
      <h2>Group config</h2>
      <label>Group ID</label>
      <input id="groupId" />
      <label>Access key</label>
      <input id="accessKey" />
      <label>Host ID</label>
      <input id="hostId" placeholder="Filled after agent login" />
      <button onclick="saveLocal()">Save locally</button>
      <button class="secondary" onclick="loadState()">Load group state</button>
    </section>

    <section>
      <h2>Agent login command</h2>
      <div class="row">
        <div><label>Display name</label><input id="displayName" value="Windows Host" /></div>
        <div><label>Device name</label><input id="deviceName" value="MSI" /></div>
      </div>
      <label>Platform</label>
      <select id="platform"><option>windows</option><option>linux</option><option>darwin</option></select>
      <button onclick="generateLogin()">Generate login command</button>
      <textarea id="loginCmd" readonly></textarea>
      <button class="secondary" onclick="copyText('loginCmd')">Copy</button>
    </section>

    <section>
      <h2>Command generator</h2>
      <label>Server directory</label>
      <input id="serverDir" value="C:\\ACBH-Test\\server-a" />
      <label>Server pack artifact ID</label>
      <input id="serverPackId" value="win-server-pack-001" />
      <label>World snapshot artifact ID</label>
      <input id="worldSnapshotId" value="win-world-safe-001" />
      <button onclick="generateCommands()">Generate commands</button>
      <textarea id="commands" readonly></textarea>
      <button class="secondary" onclick="copyText('commands')">Copy</button>
    </section>

    <section>
      <h2>Artifacts</h2>
      <label>Artifact kind</label>
      <select id="artifactKind"><option>server-pack</option><option>world-snapshot</option><option>admin-state</option></select>
      <label>Artifact ID</label>
      <input id="artifactId" />
      <button onclick="loadArtifacts()">List artifacts</button>
      <button class="secondary" onclick="loadLatest()">Load latest</button>
      <button class="secondary" onclick="loadManifest()">Load manifest</button>
    </section>

    <section class="full">
      <h2>Output</h2>
      <pre id="output">Ready.</pre>
    </section>
  </main>

<script>
const $ = (id) => document.getElementById(id);
const stateKeys = ["coordinatorUrl", "groupId", "accessKey", "hostId", "displayName", "deviceName", "platform", "serverDir", "serverPackId", "worldSnapshotId"];
function baseUrl() { return $("coordinatorUrl").value.replace(/\/$/, ""); }
function setOutput(value) { $("output").textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2); }
function saveLocal() { for (const id of stateKeys) localStorage.setItem("acbh." + id, $(id).value); setOutput("Saved local dashboard settings."); }
function restoreLocal() { $("coordinatorUrl").value = localStorage.getItem("acbh.coordinatorUrl") || location.origin; for (const id of stateKeys.slice(1)) { const value = localStorage.getItem("acbh." + id); if (value) $(id).value = value; } }
async function api(path, options = {}) { const res = await fetch(baseUrl() + path, { headers: { "content-type": "application/json", ...(options.headers || {}) }, ...options }); const text = await res.text(); let body; try { body = JSON.parse(text); } catch { body = text; } if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${typeof body === "string" ? body : JSON.stringify(body)}`); return body; }
async function refreshHealth() { try { const body = await api("/health"); $("health").textContent = "Coordinator OK"; $("healthOut").textContent = JSON.stringify(body, null, 2); } catch (e) { $("health").textContent = "Coordinator error"; $("healthOut").textContent = String(e); } }
async function refreshStorage() { try { setOutput(await api("/v1/storage/info")); } catch (e) { setOutput(String(e)); } }
async function createGroup() { try { const body = await api("/v1/groups", { method: "POST", body: JSON.stringify({ name: $("groupName").value, ownerName: $("ownerName").value }) }); $("groupId").value = body.groupId; $("accessKey").value = body.accessKey; $("groupOut").textContent = JSON.stringify(body, null, 2); saveLocal(); } catch (e) { $("groupOut").textContent = String(e); } }
async function loadState() { try { setOutput(await api(`/v1/groups/${encodeURIComponent($("groupId").value)}/state`)); } catch (e) { setOutput(String(e)); } }
async function loadArtifacts() { try { setOutput(await api(`/v1/groups/${encodeURIComponent($("groupId").value)}/artifacts`, { headers: hostHeaders() })); } catch (e) { setOutput(String(e)); } }
async function loadLatest() { try { const kind = encodeURIComponent($("artifactKind").value); setOutput(await api(`/v1/groups/${encodeURIComponent($("groupId").value)}/artifacts/latest?artifactKind=${kind}`, { headers: hostHeaders() })); } catch (e) { setOutput(String(e)); } }
async function loadManifest() { try { const g = encodeURIComponent($("groupId").value); const k = encodeURIComponent($("artifactKind").value); const a = encodeURIComponent($("artifactId").value); setOutput(await api(`/v1/groups/${g}/artifacts/${k}/${a}/manifest`, { headers: hostHeaders() })); } catch (e) { setOutput(String(e)); } }
function hostHeaders() { return { "x-acbh-host-id": $("hostId").value, "x-acbh-host-token": "copy-host-token-from-agent-config-if-needed" }; }
function generateLogin() { const exe = exeName(); $("loginCmd").value = `${exe} login ` + line(`--coordinator ${baseUrl()}`) + line(`--group-id ${$("groupId").value}`) + line(`--access-key ${$("accessKey").value}`) + line(`--name "${$("displayName").value}"`) + line(`--device-name "${$("deviceName").value}"`) + `  --platform ${$("platform").value}`; saveLocal(); }
function generateCommands() { const exe = exeName(); const dir = $("serverDir").value; const groupId = $("groupId").value; const hostId = $("hostId").value || "<host-id-from-login>"; const pack = $("serverPackId").value; const world = $("worldSnapshotId").value; $("commands").value = [
`${exe} scan ` + line(`--server-dir ${dir}`) + line(`--artifact-kind server-pack`) + line(`--artifact-id ${pack}`) + line(`--group-id ${groupId}`) + line(`--creator-host-id ${hostId}`) + `  --output ${dir}\\server-pack.manifest.json`,
`${exe} safe-sync ` + line(`--server-dir ${dir}`) + line(`--rcon-host 127.0.0.1`) + line(`--rcon-port 25575`) + line(`--rcon-password acbh-test`) + line(`--artifact-id ${world}`) + line(`--group-id ${groupId}`) + line(`--creator-host-id ${hostId}`) + line(`--server-pack-version ${pack}`) + `  --output ${dir}\\world.manifest.json`,
`${exe} push --server-dir ${dir} --manifest ${dir}\\server-pack.manifest.json`,
`${exe} push --server-dir ${dir} --manifest ${dir}\\world.manifest.json`,
`${exe} pull --artifact-kind server-pack --artifact-id ${pack} --output-dir C:\\ACBH-Test\\server-b`,
`${exe} pull --artifact-kind world-snapshot --artifact-id ${world} --output-dir C:\\ACBH-Test\\server-b`
].join("\n\n"); saveLocal(); }
function exeName() { return $("platform").value === "windows" ? ".\\acbh-agent-windows-amd64.exe" : "./acbh-agent"; }
function line(text) { return "`\n  " + text + " `\n"; }
async function copyText(id) { await navigator.clipboard.writeText($(id).value); setOutput("Copied."); }
restoreLocal(); refreshHealth();
</script>
</body>
</html>`;

export async function registerDashboardRoutes(app: FastifyInstance): Promise<void> {
  app.get("/dashboard", async (_request, reply: FastifyReply) => {
    reply.type("text/html; charset=utf-8");
    return dashboardHtml;
  });
}
