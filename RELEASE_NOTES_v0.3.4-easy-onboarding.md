# ACBH v0.3.4-easy-onboarding

本版本聚焦 Windows 桌面端首次配置体验、配置记忆、公网服务器配置简化、Group 邀请码桌面流程，并补齐 PowerShell 启动脚本兼容。

## 新增与改进

- 新增版本化 `desktop-config.json`，支持普通安装和便携模式配置记忆。
- 新增 SecretStore 抽象，Windows 使用 DPAPI CurrentUser 保存敏感凭据。
- 公网服务器配置支持仅输入 IP/域名，自动推导 Coordinator URL 和玩家入口。
- GUI 增加忘记此电脑配置、重置向导、生成/查看邀请码等入口。
- VPS bundle 新增一键安装、状态检查和升级脚本。
- Group 邀请码桌面流程支持创建、加入、列表查看和撤销。
- Minecraft 启动识别支持 `run.ps1`、`start.ps1`、`server-start.ps1`，大小写不敏感。
- PowerShell 启动脚本通过参数数组执行，避免命令字符串拼接和路径转义问题。

## 修复

- `CustomScript` 不再武断声明 Java 要求版本，当前 Java 仍通过实际 `java -version` 检测。
- GUI 服务端摘要显示 PowerShell/批处理/JAR 启动方式，不再把普通用户带到原始 JSON。
- 启动脚本选择会校验路径必须位于服务端目录内，拒绝越界和 UNC 路径。
- 服务端进程状态新增 launcher PID 记录，foreground `run.ps1` 可由现有 supervisor 启停。

## 验证

- `go -C agent test ./...`
- `corepack pnpm --filter @acbh/coordinator test`
- `powershell -ExecutionPolicy Bypass -File .\scripts\verify-windows-bundle-smoke.ps1`

## 已知限制

- PowerShell 脚本如果通过 `Start-Process` 分离 Java 子进程，目前只能有限追踪，不会误杀其他 Java 进程。
- 离线包导入和目录检测只读取脚本内容作为 evidence，不会执行脚本。
