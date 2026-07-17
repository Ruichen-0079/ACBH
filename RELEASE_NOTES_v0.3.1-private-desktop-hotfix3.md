# ACBH v0.3.1-private-desktop-hotfix3

这是 `v0.3.1-private-desktop` 的 Windows 私人桌面版 hotfix3。

## 修复

- 修复 GUI 点击“刷新状态”失败：`状态刷新失败：无效的 JSON 基元: ACBH。`
- 强制 `desktop status --json` 只向 stdout 输出纯 JSON（第一个非空字符必须是 `{`），普通中文状态文本仅在无 `--json` 时输出。
- GUI 刷新状态和 JSON 解析增加 `ConvertFrom-JsonSafe` 保护：检查 `StartsWith("{")` 或 `[`, 只解析 stdout，stderr 仅写入日志区。失败时显示中文友好错误，不再暴露 PowerShell 原始异常。
- 修复 GUI “启动 MC 服务端”失败无详情：新增 `desktop start-server --json`（和 stop-server），实现完整 preflight 检查 + 结构化失败详情 JSON。
- preflight 覆盖：serverDir 空/不存在、找不到 jar、EULA 缺失或 false、Java 不可用、端口可能占用、路径空格（使用 ArgumentList 语义）、working dir=serverDir、记录 pid 到 run/、日志到 minecraft-server.log。
- stop-server 仅停止 ACBH 记录的 pid 文件中的进程，不误杀其他 Java。
- GUI 启动 MC 失败时解析 JSON 详情，在日志区显示 message/serverDir/workingDirectory/launchCommand/javaPath/jarPath/logFile/suggestion ，弹窗显示简短中文原因，并提供打开日志目录能力。
- 公网入口未配置是正常信息状态（非错误）：`desktop status --json` 包含 `"publicEntryStatus": "not_configured"` 和对应 message；GUI 显示说明，不因其导致刷新/启动失败。
- 更新 smoke test 硬性校验上述所有要求，包括 release bundle 中 status --json 纯净性、GUI 脚本含 ConvertFrom-JsonSafe 和 --json 调用、no-jar/eula-false fixture 产生对应中文错误、无公网入口不导致失败、无敏感信息泄露。
- 不引入 Tauri，不推进公网 relay，不做大重构。
- 私人模式仍默认绑定 127.0.0.1。

## 校验

- `go test ./...`
- `corepack pnpm --filter @acbh/coordinator test`
- `powershell -ExecutionPolicy Bypass -File .\scripts\verify-windows-bundle-smoke.ps1`

所有校验通过后发布。
