# ACBH v0.3.3-simple-desktop-flow

本预发布版聚焦 Windows WinForms 桌面体验：环境自检、离线环境包导入、四步配置向导，以及“在此电脑启动 / 停止服务器”的简化主流程。

## 重点变化

- 新增 `desktop environment` 命令组：检查、修复、状态、验证离线包、导入离线包、清理 runtime cache。
- 新增 `desktop setup` 命令组：配置公网服务器、创建 Group、邀请码加入、检测 Minecraft 服务端、完成配置。
- 新增 `desktop server start-auto / stop-auto / status`，供 GUI 主按钮调用。
- GUI 改为普通用户优先的四步配置界面，原 daemon/heartbeat/relay/sync/takeover 等高级按钮默认隐藏在“高级诊断”。
- 支持 Paper、Purpur、Forge、NeoForge、Cleanroom、Fabric、Velocity、Vanilla 常见服务端入口检测，并优先识别 `run.bat` / `start.bat`。
- Coordinator 新增邀请码 API，邀请码只返回一次，服务端仅保存哈希，支持一次性邀请码和撤销。
- Windows bundle 内置私有 `runtime/node/node.exe`，普通用户运行时不需要全局 Node/npm/pnpm。

## 安全说明

- 普通桌面环境自举不会访问 GitHub、npm 或 Adoptium。
- 离线环境包导入只校验 manifest、SHA256、大小、平台和路径安全，不执行包内脚本。
- 邀请码、host token、access key、RCON 密码会在 GUI 日志中脱敏。
- 公网 relay 与 v0.3.2 远程同步能力保留。

## 已知限制

- 离线环境包的 `signature` 字段当前只做存在性检查，尚未接入真实公钥签名验签。
- current-host lease/fencing 主要复用现有 takeover/election 机制，尚未提供独立 CAS release lease API。
- 一键开服仍依赖用户的服务端 EULA、Java 与服务端包本身可正常运行。
