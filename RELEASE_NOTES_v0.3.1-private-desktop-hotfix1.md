# ACBH v0.3.1-private-desktop-hotfix1

这是 `v0.3.1-private-desktop` 的 Windows 私人桌面版 hotfix。

## 修复

- 修复 WinForms GUI 按钮点击时 BackgroundWorker 事件绑定错误导致的崩溃。
- GUI 按钮点击事件增加统一 try/catch，错误会显示中文弹窗并写入 GUI 日志区。
- Windows bundle smoke 增加 GUI 事件绑定静态检查和最小 BackgroundWorker 绑定检查。

## 校验

- `go test ./...`
- `corepack pnpm --filter @acbh/coordinator test`
- `powershell -ExecutionPolicy Bypass -File .\scripts\verify-windows-bundle-smoke.ps1`
