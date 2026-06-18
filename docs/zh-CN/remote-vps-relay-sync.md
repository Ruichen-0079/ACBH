# ACBH 远程公网模式与 VPS 中转同步

本指南描述如何使用公网 VPS 作为 Coordinator、Artifact Storage（数据同步源）和 Relay 公网入口。

## 架构

- VPS：常驻 Coordinator + Relay public listener (0.0.0.0:25565) + storage
- 本地 Windows Host (current host)：运行真实 MC server + ACBH Agent + relay host client
- 玩家：原版 MC 客户端直连 VPS_PUBLIC_IP:25565

流量：玩家 -> VPS:25565 -> relay tunnel (WS) -> host relay client -> 本地 MC 127.0.0.1:25565

## 配置 Windows Host 为远程公网模式

1. 使用 GUI 或 CLI 配置远程 Coordinator：

```powershell
.\acbh-agent-windows-amd64.exe desktop remote configure `
  --coordinator-url http://YOUR_VPS_IP:6121 `
  --public-entry YOUR_VPS_IP:25565 `
  --group-id grp_xxx
```

2. 登录组（使用 accessKey）。

3. 导入 MC 服务端目录（用于 target 推导）。

4. 启动公网中转 relay（仅 current host 有效）：

```powershell
.\acbh-agent-windows-amd64.exe desktop relay start-host
```

5. 启动 MC server (desktop start-server)。

玩家直连 VPS IP:25565 即可。

GUI 中有对应按钮和状态显示。

## VPS 部署

见 vps-deployment-runbook.md

安全组开放 25565 (玩家) 和 6121 (Coordinator，建议限制来源)。

## 注意

- 只有 current host 可启动 relay host，否则被阻止并提示中文。
- 私人本地模式继续完全可用（不影响）。
- 不打印敏感 token。
- 支持多个玩家连接（每个玩家一个 tunnel session + host client 转发到同一 MC 端口）。

## 状态 JSON 示例（远程）

```json
{
  "mode": "remote-public",
  "coordinatorUrl": "http://vps:6121",
  "dataSyncSource": "public-vps",
  "publicEntryStatus": "configured",
  "publicEntryMessage": "...",
  "relayStatus": "running",
  ...
}
```

本地模式 publicEntryStatus 仍为 not_configured，不作为错误。
