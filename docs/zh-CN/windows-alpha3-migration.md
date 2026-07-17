# Windows alpha3 迁移说明

v0.4.0-alpha3 的 Windows 桌面入口改为 Go runtime。

- 双击 `acbh-desktop-windows-amd64.exe` 会启动本地桌面 UI。
- 生产 bundle 不再包含或调用 `scripts/acbh-desktop-gui.ps1`。
- 用户不需要安装 PowerShell 7、ThreadJob、Node 或开发环境来打开 GUI。
- 本地私人 Coordinator 模式仍使用 bundle 内置 Node runtime。
- 旧 PowerShell GUI 仅作为开发回退路径保留在仓库中，可显式使用
  `acbh-desktop-windows-amd64.exe --legacy-powershell-gui`。

如果连接的是旧 Coordinator，alpha3 会通过 capability handshake 禁用不支持
的功能，并显示“当前 Coordinator 版本不支持该功能，请先升级 VPS。”

如果本地显示 owner 但服务端认证不是 owner/admin，邀请码管理会返回
`identity_mismatch`，请重新认证或修复身份后再操作。

世界备份会在执行前确认 active host lease。若租约过期且无人持有新鲜租约，
客户端会尝试重新申请；如果租约由其他 Host 持有，操作会被拒绝。
