# ACBH v0.3.1-private-desktop-hotfix1

这是 `v0.3.1-private-desktop` 的 Windows 私人桌面版 hotfix。

## 修复

- 修复点击“一键初始化”时出现“没有可用于运行脚本的运行空间 / DefaultRunspace”的问题。
- 修复 GUI 按钮事件中错误使用 BackgroundWorker / DoWork 的问题。
- GUI 命令调用改为安全的进程级执行封装，不在后台线程中执行 PowerShell ScriptBlock。
- 增强 GUI 异常捕获，避免 WinForms JIT 未处理异常弹窗。
- 不引入 Tauri。

## 校验

- `go test ./...`
- `corepack pnpm --filter @acbh/coordinator test`
- `powershell -ExecutionPolicy Bypass -File .\scripts\verify-windows-bundle-smoke.ps1`
