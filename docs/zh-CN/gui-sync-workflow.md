# GUI 同步工作流

本文说明 v0.3.1 私人桌面版中已经接通的 GUI 同步流程。

## 已可用按钮

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

## 推荐流程

1. 一键启动控制端 Coordinator。
2. 导入 MC 服务端目录并保存配置。
3. 启动 MC 服务端。
4. 检查 RCON。
5. 扫描服务端包 scan server-pack。
6. 安全同步世界快照 safe-sync。
7. 上传同步制品 push。
8. 查看日志确认结果。
9. 停止 MC 服务端、停止 daemon、一键关闭。

## current host 规则

push 必须由 current host 执行。原因是只有 current host 才代表当前有效的服务端状态，可以避免旧主机上传过期世界或服务端包。

如果 GUI 提示“当前本地主机不是 current host”，请先在控制端执行选举或接管，再重试 push。

## takeover 演练

v0.3.1 的 takeover 按钮只做安全状态检查：

- 显示当前 election status。
- 显示当前 Host 状态。
- 显示是否存在 takeover assignment。
- 给出下一步 CLI 命令。

完整自动接管仍建议使用 CLI，并先执行 dry-run。

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
