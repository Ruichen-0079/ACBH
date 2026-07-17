# ACBH v0.3.2-remote-relay-sync-hotfix1

## 修复

- 修复 GUI 点击“刷新状态”失败：`无法将“Add-GuiLog”项识别为 cmdlet...`
- 根因：`ConvertFrom-JsonSafe` 等底层 helper 中残留直接调用 `Add-GuiLog`（未定义函数），且定义顺序/作用域问题导致状态刷新时解析 JSON 前日志调用失败。
- 修复： 
  - 在脚本早期（Redact/Protect 之后，Invoke 之前）定义 `Write-GuiLogSafe`、`Add-GuiLog`（wrapper）、`Append-Log`（带 fallback）。
  - `ConvertFrom-JsonSafe`、`Invoke-Agent*`、Refresh-Status、Start-MCServer、relay/remote handlers 等底层 helper 改用 `Write-GuiLogSafe`（禁止直接 Add-GuiLog）。
  - `Add-GuiLog` 始终可用（早期定义）。
  - 所有 handler 已有 try/catch（Add-SafeClick 或内联）。
- 保留 v0.3.1-hotfix3 所有修复（status --json 纯 JSON、MC preflight 详情、DoWork/DefaultRunspace 避免）。
- 保留 v0.3.2 公网中转 relay 和远程模式完整功能（GUI 按钮、状态显示、current host 限制等）。
- `desktop status --json` 继续只输出纯 JSON。
- 不引入 Tauri、不做 P2P、不改变 relay 核心架构。

## 变更文件

- `scripts/acbh-desktop-gui.ps1` （主要）
- `scripts/verify-windows-bundle-smoke.ps1` （新增 smoke 硬性检查）

## 校验

- go test ./... passed
- corepack pnpm --filter @acbh/coordinator test passed
- powershell smoke passed (含新 Add-GuiLog / Write-GuiLogSafe / Convert 位置检查)

## 注意

- 私人本地模式、远程公网模式均正常。
- 只有 current host 可启动 relay。
- Release notes 提及限制保持不变。

## Assets

- acbh-desktop-windows-amd64.exe
- acbh-agent-windows-amd64.exe
- acbh-v0.3.2-remote-relay-sync-hotfix1-bundle.zip
- acbh-coordinator-linux-amd64-bundle.tar.gz
- SHA256SUMS
