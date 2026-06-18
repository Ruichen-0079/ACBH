# ACBH v0.3.1-private-desktop

Windows 私人桌面预览版的 GUI 同步流程增强版。

## 重点变化

- 接通 WinForms GUI 中 v0.3 标记为“暂不可用”的高级按钮。
- 新增 desktop CLI 封装命令，供 GUI 安全调用。
- 支持后台服务 daemon 的 start / stop / status 和 pid file 管理。
- 支持扫描服务端包 scan server-pack。
- 支持 RCON 检查和 safe-sync 世界快照。
- 支持 push 前 current host 检查。
- 支持 pull latest 的安全确认入口。
- 接管 takeover 按钮改为状态检查和 CLI 引导，不自动执行危险接管。
- GUI 增加日志显示、刷新日志、打开日志目录和清空 GUI 显示。
- GUI 文案改为中文优先，并保留英文术语对照。

## 资产

- `acbh-desktop-windows-amd64.exe`
- `acbh-agent-windows-amd64.exe`
- `acbh-v0.3.1-private-desktop-bundle.zip`
- `SHA256SUMS`

## 安全说明

- 默认仍只绑定 `127.0.0.1`。
- GUI 不打印 `accessKey`、`hostToken` 或 RCON password。
- safe-sync 使用隐藏输入框和 `ACBH_RCON_PASSWORD` 进程环境变量传递 RCON 密码。
- push 会提前检查 current host，不满足时阻断。
- pull 默认不应用删除项，并提示用户备份。
- GUI 不会误杀非 ACBH 启动的 Java 进程。

## 已知限制

- takeover 仍是演练/状态检查入口，完整自动接管建议使用 CLI dry-run 后执行。
- pull GUI 默认拉取最新 `world-snapshot`，高级制品选择仍建议使用 CLI。
