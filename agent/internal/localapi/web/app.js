const byId = (id) => document.getElementById(id);
let lastStatus = null;
let diagnosticsText = "";

async function api(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { "Content-Type": "application/json", ...(options.headers || {}) } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.message || body.error || `HTTP ${response.status}`);
  return body;
}

function show(id) {
  for (const section of ["offline", "online", "progress"]) byId(section).classList.toggle("hidden", section !== id);
}

function stateLabel(state, healthy) {
  return healthy.includes(state) ? "● 正常" : `● ${state || "未知"}`;
}

function formatDuration(seconds) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days) return `${days}天${hours}小时`;
  if (hours) return `${hours}小时${minutes}分钟`;
  return `${minutes}分钟`;
}

function render(status) {
  lastStatus = status;
  const active = ["CONNECTING", "RECONNECTING", "STOPPING"].includes(status.overall) || status.current_step;
  if (active) {
    show("progress");
    byId("step").textContent = status.current_step || status.user_message || "正在处理";
    return;
  }
  if (status.overall === "ONLINE") {
    show("online");
    byId("endpoint").textContent = status.public_endpoint || "正在获取地址";
    byId("minecraft-state").textContent = stateLabel(status.minecraft?.state, ["READY"]);
    byId("relay-state").textContent = stateLabel(status.relay?.state, ["ONLINE"]);
    byId("uptime").textContent = formatDuration(status.uptime_seconds || 0);
  } else {
    show("offline");
    byId("offline").querySelector("h2").textContent = status.user_message || "当前没有托管服务器";
  }
}

async function refresh() {
  try { render(await api("/local/v1/status")); }
  catch (error) { show("offline"); byId("offline").querySelector("h2").textContent = `Agent 不可用：${error.message}`; }
}

async function action(path) {
  const operation = await api(path, { method: "POST" });
  show("progress");
  await refresh();
  return operation;
}

byId("start").addEventListener("click", () => action("/local/v1/start").catch((e) => alert(e.message)));
byId("stop").addEventListener("click", () => action("/local/v1/stop").catch((e) => alert(e.message)));
byId("copy-address").addEventListener("click", () => navigator.clipboard.writeText(lastStatus?.public_endpoint || ""));

byId("open-settings").addEventListener("click", async () => {
  try {
    const config = await api("/local/v1/config");
    byId("coordinator-host").value = config.coordinator_host || "";
    byId("coordinator-port").value = config.coordinator_port || 6121;
  } catch {}
  byId("access-token").value = "";
  byId("settings-dialog").showModal();
});
byId("close-settings").addEventListener("click", () => byId("settings-dialog").close());

byId("settings-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (event.submitter?.id !== "save-settings") return;
  const token = byId("access-token").value;
  if (!token) { byId("settings-result").textContent = "请输入 Access Token（已保存的 Token 不会回显）。"; return; }
  try {
    await api("/local/v1/config", { method: "PUT", body: JSON.stringify({
      coordinator_host: byId("coordinator-host").value,
      coordinator_port: Number(byId("coordinator-port").value || 6121),
      access_token: token,
    }) });
    byId("settings-result").textContent = "设置已保存。";
  } catch (error) { byId("settings-result").textContent = error.message; }
});

byId("import-server").addEventListener("click", async (event) => {
  event.preventDefault();
  try {
    const result = await api("/local/v1/import", { method: "POST", body: JSON.stringify({ server_dir: byId("server-dir").value }) });
    byId("settings-result").textContent = `导入成功：${result.server_dir}`;
  } catch (error) { byId("settings-result").textContent = error.message; }
});

byId("open-diagnostics").addEventListener("click", async () => {
  const dialog = byId("diagnostics-dialog");
  dialog.showModal();
  try { diagnosticsText = JSON.stringify(await api("/local/v1/diagnostics"), null, 2); }
  catch (error) { diagnosticsText = error.message; }
  byId("diagnostics").textContent = diagnosticsText;
});
byId("close-diagnostics").addEventListener("click", () => byId("diagnostics-dialog").close());
byId("copy-diagnostics").addEventListener("click", () => navigator.clipboard.writeText(diagnosticsText));
byId("download-diagnostics").addEventListener("click", () => {
  const url = URL.createObjectURL(new Blob([diagnosticsText], { type: "application/json" }));
  const link = document.createElement("a"); link.href = url; link.download = "acbh-diagnostics.json"; link.click(); URL.revokeObjectURL(url);
});

refresh();
setInterval(refresh, 2000);
