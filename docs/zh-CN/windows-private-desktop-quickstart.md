# ACBH Windows 私人桌面版快速开始

适用对象：只想在自己的 Windows 电脑上管理私人 Minecraft 服务端的用户。

## 下载和启动

1. 下载 `acbh-v0.3.1-private-desktop-bundle.zip`。
2. 解压到普通用户可写目录，例如 `D:\Apps\ACBH`。
3. 双击 `acbh-desktop-windows-amd64.exe`。

点击“一键启动”后，ACBH 会：

- 只在 `127.0.0.1:6121` 启动控制端 Coordinator。
- 在 `%APPDATA%\ACBH` 保存状态、日志、服务器组 Group 和主机 Host 身份。
- 第一次启动时创建私人本地服务器组。
- 后续启动时复用同一个 `groupId`、`accessKey` 和 `hostToken`。
- 自动注册本机 Agent 并发送一次心跳 heartbeat。

如果 exe 同级存在 `portable.flag`，ACBH 会进入便携模式，状态写入当前目录的 `data\`。

## v0.3.1 已接通的 GUI 按钮

- 一键初始化
- 一键启动
- 一键关闭
- 刷新状态
- 打开日志目录
- 导入 MC 服务端目录
- 保存导入配置
- 启动 MC 服务端
- 停止 MC 服务端
- 发送心跳 heartbeat
- 启动后台服务 daemon
- 停止后台服务 daemon
- 扫描服务端包 scan server-pack
- 安全同步世界快照 safe-sync
- 上传同步制品 push
- 拉取同步制品 pull
- 接管演练 takeover
- 检查远程控制 RCON
- 刷新日志
- 清空 GUI 显示
- 重置本地配置

v0.3.1 的接管演练 takeover 是安全状态检查入口。它会显示 election status、当前 Host 和 takeover assignment，不会自动执行危险接管。完整接管仍建议先使用 CLI dry-run。

## 推荐 GUI 闭环

1. 点击“一键启动”。
2. 点击“导入 MC 服务端目录”，选择包含 `server.properties`、`world\`、`mods\` 的目录。
3. 点击“保存导入配置”。
4. 点击“启动 MC 服务端”。
5. 点击“检查远程控制 RCON”，按提示开启 RCON。
6. 点击“安全同步世界快照”，在隐藏输入框填写 RCON 密码。
7. 点击“上传同步制品 push”。
8. 完成后点击“停止 MC 服务端”和“一键关闭”。

push 上传前必须是当前主机 Current Host。这样可以避免旧主机或离线主机覆盖最新同步制品。如果 GUI 提示“不是 current host”，请先在控制端完成选举/接管，或使用 CLI 高级流程。

## RCON 配置

safe-sync 会通过 RCON 执行 `save-all flush`，确保世界文件落盘后再生成世界快照。

在 `server.properties` 中配置：

```properties
enable-rcon=true
rcon.port=25575
rcon.password=请换成你自己的强密码
```

ACBH 不会自动写入 RCON 密码，不会在 GUI 日志、普通日志或命令行参数中明文打印密码。GUI 会使用隐藏输入框，并通过 `ACBH_RCON_PASSWORD` 进程环境变量传递给子命令。

## 日志

GUI 的日志区显示当前窗口最近输出，并提供：

- 刷新日志：读取 ACBH logs 目录内最新日志文件的最近 100 行。
- 打开日志目录：打开 `%APPDATA%\ACBH\logs` 或便携模式 `data\logs`。
- 清空 GUI 显示：只清空窗口显示，不删除真实日志文件。

日志显示会隐藏 `accessKey`、`hostToken`、RCON password 和形如 `ak_...`、`ht_...` 的密钥。

## 安全关闭

推荐顺序：

1. 停止 MC 服务端。
2. 停止后台服务 daemon。
3. 一键关闭控制端 Coordinator。

关闭不会删除私人服务器组、加入密钥或主机令牌。只有明确执行下面命令才会重置本地身份：

```powershell
.\acbh-agent-windows-amd64.exe desktop reset --yes
```

## GUI 术语对照表

| 英文 | 中文解释 |
| --- | --- |
| Coordinator | 控制端 |
| Agent | 本地主机代理 |
| Group | 服务器组 |
| Host | 主机 |
| Current Host | 当前主机 |
| heartbeat | 心跳 |
| daemon | 后台心跳服务 |
| scan | 扫描 |
| safe-sync | 安全同步 |
| push | 上传同步制品 |
| pull | 拉取同步制品 |
| takeover | 接管 |
| artifact | 同步制品 |
| manifest | 文件清单 |
| server-pack | 服务端包 |
| world-snapshot | 世界快照 |
| server-runtime | 完整服务端运行目录 |
| RCON | Minecraft 远程控制接口 |
| accessKey | 加入密钥 |
| hostToken | 主机令牌 |
