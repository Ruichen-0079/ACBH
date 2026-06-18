# Windows 常见问题与修复

## 控制端缺少 ws

错误示例：

```text
ERR_MODULE_NOT_FOUND ws
Cannot find package 'ws'
```

含义：控制端 Coordinator 运行时缺少 WebSocket 依赖。

处理：

1. 优先重新下载完整的 `acbh-v0.3.1-private-desktop-bundle.zip`。
2. 如果你是开发者，确认 `apps/coordinator/package.json` 中 `ws` 在 `dependencies`，不是只在 `devDependencies`。
3. 重新执行 release bundle smoke test。

## 401 Invalid access key

含义：加入密钥 accessKey 与当前服务器组 Group 不匹配。

处理：

1. 私人模式下优先运行桌面入口，让程序自动复用本地保存的身份。
2. 如果你删除过 `coordinator-state.json`，但保留了 `config.yaml`，请选择恢复、重建或清空本地配置。
3. 不要从旧截图或旧终端输出复制 `accessKey`。

## not current host

含义：当前本地主机 Host 不是 current host，不能上传同步制品 push。

处理：

1. 在控制端 Coordinator 执行选举/接管。
2. 或使用 CLI 高级流程确认当前主机状态。
3. 再回到 GUI 点击“上传同步制品 push”。

## RCON 未开启

safe-sync 需要 RCON 执行 `save-all flush`。如果 GUI 显示 RCON 未开启，请在 `server.properties` 中配置：

```properties
enable-rcon=true
rcon.port=25575
rcon.password=请换成你自己的强密码
```

不要把 RCON 密码写进 PowerShell 命令历史或公开日志。优先使用 GUI 隐藏输入框。

## RCON password is required

含义：需要 RCON 密码。safe-sync 必须通过 RCON 执行 `save-all flush` 后才能安全生成世界快照。

处理：

1. 确认 `server.properties` 中 `rcon.password` 非空。
2. 在 GUI 的隐藏输入框输入密码。
3. 不要把密码作为普通命令行参数公开给别人。

## MC 服务端没有停止

GUI 只会停止由 ACBH 启动的 MC 服务端。它不会误杀用户自己启动的 Java 进程。

如果状态文件损坏：

```powershell
.\acbh-agent-windows-amd64.exe server repair-state
```

只在确认旧服务端已经停止后使用 repair-state。

## 日志里看到英文

GUI 会优先显示中文，并在必要处保留英文术语用于排错。例如：

- 后台服务 daemon
- 安全同步 safe-sync
- 上传同步制品 push
- 当前主机 Current Host

如果弹窗包含英文错误原文，前面会有中文解释。日志显示会隐藏 `accessKey`、`hostToken`、RCON password 和 `ak_...` / `ht_...`。

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
