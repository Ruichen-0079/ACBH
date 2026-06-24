# ACBH v0.3.3-simple-desktop-flow-hotfix3

本热修复聚焦 Windows 简化桌面流中的 Minecraft 服务端启动识别与 GUI 展示问题。

## 修复内容

- 拆分服务端目录检查和启动就绪状态，新增 `inspectionOk`、`launchReady`、`blockingReasons`。
- 新增 `LaunchProfile`，明确记录启动类型、启动脚本或核心 JAR、识别证据和 Java 兼容性。
- 修复 MCSL2/Fabric launcher 等服务端目录被识别为 `Unknown` 或状态自相矛盾的问题。
- 新增启动入口选择命令：
  - `desktop server candidates --json`
  - `desktop server select-launch --path ... --json`
  - `desktop server launch-profile --json`
  - `desktop server clear-launch-profile --json`
- 拆分 `requiredJavaVersion` 和 `detectedJavaVersion`，实际执行 `java -version` 检测当前 Java。
- GUI 普通界面显示用户可读摘要，不再直接展示整段原始 JSON；高级信息可通过识别证据查看。
- GUI 增加自动推荐、选择启动脚本、选择核心 JAR、打开服务端目录和查看识别证据入口。

## 验证

- `go -C agent test ./...`
- `corepack pnpm --filter @acbh/coordinator test`
- `powershell -ExecutionPolicy Bypass -File .\scripts\verify-windows-bundle-smoke.ps1`
