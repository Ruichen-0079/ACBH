# ACBH v0.3.2-remote-relay-sync

公网中转与数据同步源。

## 新增

- 公网 VPS 作为 Coordinator + Artifact Storage + Relay 公网入口 (0.0.0.0:25565)
- Windows Host desktop relay start-host / stop-host / status (仅 current host 可启动，否则中文阻断)
- 远程公网模式：desktop remote configure / status
- GUI 支持远程公网模式配置、启动/停止公网中转 relay、复制玩家连接地址、状态显示
- Coordinator 支持 ACBH_RELAY_PUBLIC_HOST / PORT 常驻 public relay ingress
- 自动发现 tunnel sessions，host manager 为 assigned sessions 启动转发
- 支持多玩家 TCP 连接
- VPS bundle (linux) + 部署文档 + runbook
- status --json 增强 mode / dataSyncSource / publicEntry* / relayStatus 等
- 私人本地模式完全保留，不受影响

## 变更

- 复用现有 relay host / player / tunnel 架构
- 新增 list tunnel sessions API
- Public relay ingress 在 coordinator 进程内启动

## 注意

- 不引入 Tauri / P2P
- 不破坏 hotfix3 JSON / MC preflight
- 玩家使用原版客户端直连 VPS IP:25565
- 仅 current host 启动 relay

## 校验

- go test ./...
- pnpm coordinator test
- windows smoke (含 GUI 文字、remote/relay 检查)

## Assets

- acbh-*-windows-amd64.exe
- acbh-v0.3.2-remote-relay-sync-bundle.zip
- acbh-coordinator-linux-amd64-bundle.tar.gz
- SHA256SUMS
