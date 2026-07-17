const byId = (id) => document.getElementById(id);
let lastStatus = null;
let publicConfig = null;
let detailsSnapshot = { events: [], logs: [] };

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.message || body.error || `HTTP ${response.status}`);
    error.code = body.error || "request_failed";
    throw error;
  }
  return body;
}

const stateText = {
  ONLINE: "在线", CONNECTING: "连接中", RECONNECTING: "重连中", OFFLINE: "离线",
  READY: "已检测到", UNKNOWN: "检测中", DEGRADED: "异常", ERROR: "异常",
  AUTH_FAILED: "认证失败", INCOMPATIBLE: "版本不兼容", STOPPING: "停止中",
};

function textFor(state, fallback = "未知") {
  return stateText[state] || state || fallback;
}

function chineseError(error) {
  const value = `${error?.code || ""} ${error?.message || error || ""}`.toLowerCase();
  if (value.includes("auth") || value.includes("token") || value.includes("401") || value.includes("403")) return "Access Token 无效，请重新填写。";
  if (value.includes("public_port_in_use") || value.includes("already in use") || value.includes("端口已被占用")) return "VPS 公网端口已被占用，请更换端口。";
  if (value.includes("protocol") || value.includes("incompatible")) return "Agent 与 Coordinator 版本不兼容。";
  if (value.includes("config_locked")) return "请先停止中转，再修改配置。";
  if (value.includes("fetch") || value.includes("network") || value.includes("connect")) return "网络连接失败，Agent 会继续尝试恢复。";
  return error?.message || "操作失败，请查看详情。";
}

function render(status) {
  lastStatus = status;
  byId("agent-state").textContent = status.agent?.state === "ONLINE" ? "正常" : "异常";
  byId("local-state").textContent = status.local_server?.state === "READY" ? "已检测到" : status.local_server?.state === "OFFLINE" ? "未检测到" : "检测中";
  byId("relay-state").textContent = status.relay?.state === "ONLINE" ? "在线" : status.relay?.state === "RECONNECTING" || status.relay?.state === "CONNECTING" ? "重连中" : "离线";
  byId("public-address").textContent = status.public_endpoint || configuredAddress() || "—";

  const pill = byId("overall-pill");
  pill.textContent = textFor(status.overall, "未启动");
  pill.className = `pill ${status.overall === "ONLINE" ? "good" : ["CONNECTING", "RECONNECTING"].includes(status.overall) ? "warn" : "neutral"}`;

  const error = firstError(status);
  const errorNode = byId("short-error");
  errorNode.textContent = error;
  errorNode.classList.toggle("hidden", !error);

  byId("detail-agent").textContent = textFor(status.agent?.state);
  byId("detail-local").textContent = textFor(status.local_server?.state);
  byId("detail-relay").textContent = textFor(status.relay?.state);
  byId("detail-coordinator").textContent = textFor(status.coordinator?.state);
  byId("detail-local-endpoint").textContent = status.local_endpoint || "—";
  byId("detail-public-endpoint").textContent = status.public_endpoint || configuredAddress() || "—";
  byId("agent-version").textContent = status.agent_version || "—";
  byId("last-error").textContent = error || "暂无错误";
}

function firstError(status) {
  for (const component of [status.relay, status.coordinator, status.local_server]) {
    if (["ERROR", "AUTH_FAILED", "INCOMPATIBLE", "DEGRADED"].includes(component?.state)) {
      return chineseError({ code: component.reason_code, message: component.user_message || component.technical_message });
    }
  }
  return "";
}

function configuredAddress() {
  const host = byId("public-host").value.trim();
  const port = Number(byId("custom-port").value || 25565);
  return host ? `${host}:${port}` : "";
}

async function loadConfig() {
  try {
    publicConfig = await api("/local/v1/config");
    byId("public-host").value = publicConfig.coordinator_host || "";
    byId("custom-port").value = publicConfig.public_minecraft_port || 25565;
    byId("token-state").textContent = publicConfig.access_token_configured ? "Token 已配置" : "Token 未配置";
    byId("token-state").classList.toggle("configured", !!publicConfig.access_token_configured);
    byId("access-token").placeholder = publicConfig.access_token_configured ? "已配置；留空则保持不变" : "请输入 ACBH Access Token";
  } catch {
    publicConfig = { access_token_configured: false };
  }
}

async function waitOperation(operation) {
  if (!operation?.operation_id) return;
  for (let attempt = 0; attempt < 300; attempt += 1) {
    const current = await api(`/local/v1/operations/${encodeURIComponent(operation.operation_id)}`);
    if (current.status === "SUCCEEDED") return current;
    if (current.status === "FAILED") throw Object.assign(new Error(current.error || "操作失败"), { code: current.error_code });
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("操作等待超时");
}

async function stopIfConfigurationChanges(next) {
  const changed = publicConfig && (
    publicConfig.coordinator_host !== next.coordinator_host ||
    Number(publicConfig.minecraft_local_port) !== next.minecraft_local_port ||
    Number(publicConfig.public_minecraft_port) !== next.public_minecraft_port ||
    !!next.access_token
  );
  if (changed && lastStatus && !["OFFLINE", "ERROR"].includes(lastStatus.relay?.state)) {
    byId("action-message").textContent = "配置有变化，正在平滑重启中转…";
    await waitOperation(await api("/local/v1/stop", { method: "POST" }));
  }
}

byId("connection-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const port = Number(byId("custom-port").value || 25565);
  const next = {
    coordinator_host: byId("public-host").value.trim(),
    coordinator_port: 6121,
    access_token: byId("access-token").value,
    minecraft_local_port: port,
    public_minecraft_port: port,
  };
  if (!next.access_token && !publicConfig?.access_token_configured) {
    byId("action-message").textContent = "首次使用必须填写 Access Token。";
    return;
  }
  setBusy(true);
  try {
    await stopIfConfigurationChanges(next);
    publicConfig = await api("/local/v1/config", { method: "PUT", body: JSON.stringify(next) });
    byId("access-token").value = "";
    byId("token-state").textContent = "Token 已配置";
    byId("token-state").classList.add("configured");
    byId("action-message").textContent = "配置已保存，正在启动公网中转…";
    await waitOperation(await api("/local/v1/start", { method: "POST" }));
    byId("action-message").textContent = "公网中转已启动。即使本地服务器暂未启动，中转也会保持运行。";
    await refresh();
  } catch (error) {
    byId("action-message").textContent = chineseError(error);
  } finally {
    setBusy(false);
  }
});

byId("stop-relay").addEventListener("click", async () => {
  setBusy(true);
  try {
    await waitOperation(await api("/local/v1/stop", { method: "POST" }));
    byId("action-message").textContent = "公网中转已停止；本地 Minecraft 不受影响。";
    await refresh();
  } catch (error) {
    byId("action-message").textContent = chineseError(error);
  } finally {
    setBusy(false);
  }
});

function setBusy(busy) {
  byId("save-start").disabled = busy;
  byId("stop-relay").disabled = busy;
}

byId("copy-address").addEventListener("click", async () => {
  const address = lastStatus?.public_endpoint || configuredAddress();
  if (!address) return;
  await navigator.clipboard.writeText(address);
  byId("action-message").textContent = "公网地址已复制。";
});

byId("details").addEventListener("toggle", () => {
  if (byId("details").open) refreshDetails();
});

async function refreshDetails() {
  if (!byId("details").open || byId("pause-logs").checked) return;
  try {
    const [events, logs, diagnostics] = await Promise.all([
      api("/local/v1/events?limit=200"),
      api("/local/v1/logs?limit=200"),
      api("/local/v1/diagnostics"),
    ]);
    detailsSnapshot = { events: events.events || [], logs: logs.logs || [] };
    byId("frpc-version").textContent = diagnostics.frpc_version?.version || "—";
    renderEvents();
    renderLogs();
  } catch (error) {
    byId("logs").textContent = chineseError(error);
  }
}

function renderEvents() {
  const entries = detailsSnapshot.events.slice(-12).reverse();
  byId("state-events").innerHTML = "";
  for (const event of entries) {
    const item = document.createElement("li");
    item.textContent = `${new Date(event.time).toLocaleTimeString()} · ${event.component} · ${event.from} → ${event.to} · ${event.reason}`;
    byId("state-events").appendChild(item);
  }
  if (!entries.length) byId("state-events").innerHTML = "<li>暂无状态变化</li>";
}

function sanitizedLogText() {
  const filter = byId("log-filter").value;
  const eventLines = detailsSnapshot.events
    .filter((event) => filter === "all" || event.component === filter)
    .slice(-200)
    .map((event) => JSON.stringify({ time: event.time, component: event.component, from: event.from, to: event.to, reason: event.reason }));
  const operationLines = filter === "all" ? detailsSnapshot.logs.slice(-200).map((message) => JSON.stringify({ event: "agent_operation", message })) : [];
  return [...eventLines, ...operationLines]
    .join("\n")
    .replace(/Bearer\s+[^\s"']+/gi, "Bearer [REDACTED]")
    .replace(/("?(?:access[_-]?token|authorization)"?\s*[:=]\s*)"?[^,"\s}]+"?/gi, "$1[REDACTED]");
}

function renderLogs() {
  byId("logs").textContent = sanitizedLogText() || "暂无日志";
  byId("logs").scrollTop = byId("logs").scrollHeight;
}

byId("log-filter").addEventListener("change", renderLogs);
byId("pause-logs").addEventListener("change", () => { if (!byId("pause-logs").checked) refreshDetails(); });
byId("copy-logs").addEventListener("click", () => navigator.clipboard.writeText(sanitizedLogText()));
byId("open-log-dir").addEventListener("click", async () => {
  try {
    const result = await api("/local/v1/logs/open", { method: "POST" });
    if (!result.opened && result.protocol) window.location.href = result.protocol;
  } catch (error) {
    byId("action-message").textContent = chineseError(error);
  }
});
byId("export-diagnostics").addEventListener("click", () => { window.location.href = "/local/v1/diagnostics/export"; });

async function refresh() {
  try {
    render(await api("/local/v1/status"));
    if (byId("details").open) await refreshDetails();
  } catch (error) {
    byId("agent-state").textContent = "异常";
    byId("short-error").textContent = `Agent 异常：${chineseError(error)}`;
    byId("short-error").classList.remove("hidden");
  }
}

(async () => {
  await loadConfig();
  await refresh();
  setInterval(refresh, 2000);
})();
