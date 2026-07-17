# ACBH v0.3.3-simple-desktop-flow-hotfix1

本 hotfix 修复 Windows PowerShell 5.1 启动 WinForms GUI 时因 UTF-8 无 BOM 解码导致的中文乱码和 ParserError。

## 修复

- `scripts/acbh-desktop-gui.ps1` 改为 UTF-8 BOM 编码，兼容 Windows PowerShell 5.1 的 `-File` 解码规则。
- release bundle 构建时强制将 GUI 脚本写入 UTF-8 BOM。
- GUI 增加 `-SelfTest` 参数，用于无窗口解析/启动自检。
- Windows bundle smoke test 增加：
  - 检查 bundle 内 GUI 脚本前三字节为 UTF-8 BOM。
  - 使用真实 `powershell.exe -NoProfile -ExecutionPolicy Bypass -STA -File ... -SelfTest` 验证，不再只做内存 parser 检查。

## 说明

- 不引入 Tauri。
- 不改变 v0.3.3 的桌面流程、relay、sync、current-host 逻辑。
- 普通用户仍使用 `acbh-desktop-windows-amd64.exe` 启动 GUI。
