# ACBH v0.3-private-desktop

这是 ACBH 面向 Windows 私人本地部署的一键桌面预览版。

本版本重点解决普通用户本地部署时的启动、依赖、配置、中文提示和 Minecraft 服务端导入问题。用户解压后可直接双击 `acbh-desktop-windows-amd64.exe` 打开桌面窗口，不再需要手动启动 Coordinator、执行 npm/pnpm/corepack，或复制 groupId/accessKey。

## 新增

- 新增 Windows WinForms 桌面 GUI：
  - 一键初始化
  - 一键启动
  - 一键关闭
  - 刷新状态
  - 打开日志目录
  - 导入 Minecraft 服务端目录
  - 保存导入配置
  - 启动/停止 MC 服务端
  - 发送 heartbeat
  - 重置本地配置

- 新增 `acbh-desktop-windows-amd64.exe`：
  - 双击默认打开桌面 GUI
  - `--cli` 保留命令行一键启动模式
  - GUI 脚本缺失时回退到命令行模式

- 新增私人桌面模式：
  - 默认绑定 `127.0.0.1`
  - 自动启动/复用 Coordinator
  - 自动创建/复用私人服务器组
  - 自动注册/复用本地主机身份
  - groupId、accessKey、hostId、hostToken 持久化
  - 一键关闭时保留本地身份

- 新增 Minecraft 服务端目录检测：
  - Fabric / Paper / Vanilla / Velocity 识别
  - `server.properties` 检测
  - `eula.txt` 检测
  - RCON 配置检测
  - Java 检测
  - 中文可操作提示

- 改进 release bundle：
  - 包含 `acbh-desktop-windows-amd64.exe`
  - 包含 `acbh-agent-windows-amd64.exe`
  - 包含 WinForms GUI 脚本
  - 包含 Coordinator 运行文件
  - 包含生产依赖
  - 生成 `SHA256SUMS`
  - 普通用户无需手动安装 npm/pnpm/corepack

## 修复

- 修复 Coordinator 运行时缺少 `ws` 的问题。
- 修复生产依赖安装漏掉运行时依赖的问题。
- 改进 Windows 普通权限下的启动体验。
- 改进 groupId/accessKey 易错流程，私人模式下不再要求用户每次复制密钥。
- 改进中文错误提示。
- 改进日志目录打开逻辑。
- 改进便携模式/自定义数据目录下 GUI 子命令读取配置的问题。

## 暂不可用 / 限制

以下高级功能在 GUI 中已显示为“暂不可用”，不会执行半成品操作：

- 启动/停止 daemon
- scan 服务端包
- safe-sync 世界快照
- push / pull 制品
- takeover 演练

如需使用这些高级能力，请暂时通过 CLI 执行。

本版本 GUI 使用 Windows 自带 WinForms + PowerShell 实现，没有引入 Wails/Tauri。Tauri 将另开 `feature/tauri-desktop-spike` 分支验证，不阻塞当前 v0.3 发布。

## 已验证

- `go test ./...`
- `corepack pnpm --filter @acbh/coordinator test`
- `powershell -ExecutionPolicy Bypass -File .\scripts\verify-windows-bundle-smoke.ps1`
- GUI 脚本 UTF-8 PowerShell parser 检查通过

## 建议使用方式

1. 下载 `acbh-v0.3-private-desktop-bundle.zip`
2. 解压到任意目录
3. 双击 `acbh-desktop-windows-amd64.exe`
4. 点击“一键初始化”
5. 点击“一键启动”
6. 导入 Minecraft 服务端目录
7. 根据 GUI 提示检查 Java、EULA、RCON 状态

本版本建议仅用于本机或可信局域网私人环境，不建议直接公网暴露。
