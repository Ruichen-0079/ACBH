# ACBH Windows 私人桌面版快速开始

适用对象：只想在自己的 Windows 电脑上管理私人 Minecraft 服务端的用户。

## 下载和启动

1. 下载 `acbh-v0.3-private-desktop-bundle.zip`。
2. 解压到一个普通用户可写目录，例如 `D:\Apps\ACBH`。
3. 双击 `acbh-desktop-windows-amd64.exe`。

正常情况下会打开“ACBH 私人本地桌面版”窗口。点击“一键启动”后，桌面入口会自动完成：

- 使用 `127.0.0.1:6121` 启动控制端。
- 使用 `%APPDATA%\ACBH` 保存状态、日志、服务器组和主机身份。
- 第一次启动时创建私人本地服务器组。
- 后续启动时复用同一个 `groupId`、`accessKey` 和 `hostToken`。
- 自动注册本机主机并发送一次心跳。

如果 exe 同级存在 `portable.flag`，ACBH 会进入便携模式，状态写入当前目录的 `data\`。

## 成功标志

一键启动成功后，你会看到：

- GUI 中“控制端状态”为“运行中”。
- GUI 中“本地主机代理”为“已登录 / heartbeat 可用”。
- GUI 中显示当前服务器组和主机 ID。
- 日志窗口显示 `控制端已启动，/health 正常。`
- 日志窗口显示 `心跳已发送，主机已在控制端可见。`

如果你需要排错，也可以使用命令行模式：

```powershell
.\acbh-desktop-windows-amd64.exe --cli
```

不要把日志或截图里的密钥发给别人。ACBH 不会主动把 `accessKey`、`hostToken`、RCON 密码写进普通日志。

## GUI 按钮可用性

当前 v0.3 pre-release 中，以下按钮是真实可用的：

- 一键初始化
- 一键启动
- 一键关闭
- 刷新状态
- 打开日志目录
- 导入 MC 目录
- 保存导入配置
- 启动 MC 服务端
- 停止 MC 服务端
- 发送 heartbeat
- 重置本地配置

以下按钮会在界面上标注“暂不可用”，点击后只显示说明，不会执行危险或不完整操作：

- 启动/停止 daemon
- scan 服务端包
- safe-sync 世界快照
- push / pull 制品
- takeover 演练

## 关闭

推荐使用：

```powershell
.\acbh-agent-windows-amd64.exe desktop stop
```

也可以在 GUI 中点击“一键关闭”。

关闭不会删除私人服务器组、加入密钥或主机令牌。只有明确执行下面命令才会重置本地身份：

```powershell
.\acbh-agent-windows-amd64.exe desktop reset --yes
```

## 不再需要手动执行

普通私人模式不再要求你手动运行：

- `npm install`
- `pnpm install`
- `corepack enable`
- `node index.js`
- 多行 PowerShell 反引号命令
- 手动复制 `groupId` / `accessKey`

文档中也不再使用 `<host-id>` 这类 PowerShell 会误解析的占位符。需要示例时使用 `YOUR_HOST_ID`。

## 数据目录

默认目录：

```text
%APPDATA%\ACBH
```

主要文件：

- `config.yaml`：本地主机代理配置。
- `private-local-state.json`：私人模式服务器组和主机身份。
- `coordinator-state.json`：控制端状态。
- `logs\coordinator.log`：控制端日志。

如果 `config.yaml` 还在，但 `coordinator-state.json` 被删除，启动时会提示控制端状态丢失。此时请选择恢复、重建或清空本地配置，不要继续复制旧密钥反复登录。
