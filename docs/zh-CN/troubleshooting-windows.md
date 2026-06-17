# Windows 常见问题与修复

## 控制端缺少 ws

错误示例：

```text
ERR_MODULE_NOT_FOUND ws
Cannot find package 'ws'
```

含义：控制端运行时缺少 WebSocket 依赖。

处理：

1. 优先重新下载完整的 `acbh-v0.3-private-desktop-bundle.zip`。
2. 如果你是开发者，确认 `apps/coordinator/package.json` 中 `ws` 在 `dependencies`，不是只在 `devDependencies`。
3. 重新执行 release bundle smoke test。

## corepack 写入 Program Files 失败

错误示例：

```text
EPERM C:\Program Files\nodejs\pnpm
```

含义：当前 Windows 账户无权修改 Node.js 全局目录。

私人桌面版不要求普通用户执行 `corepack enable`，也不要求管理员权限。请使用完整 bundle，或让程序使用本地工具目录。

## 401 Invalid access key

含义：加入密钥与当前服务器组不匹配。

处理：

1. 私人模式下优先运行桌面入口，让程序自动复用本地保存的身份。
2. 如果你删除过 `coordinator-state.json`，但保留了 `config.yaml`，请选择恢复、重建或清空本地配置。
3. 不要从旧截图或旧终端输出复制 `accessKey`。

## 控制端状态丢失

提示示例：

```text
本地主机配置存在，但控制端没有对应服务器组。可能是控制端状态文件被删除。请选择恢复、重建或清空本地配置。
```

处理：

- 如果有备份，把原来的 `coordinator-state.json` 放回 `%APPDATA%\ACBH`。
- 如果没有备份，执行 `desktop reset --yes` 后重新初始化私人模式。

## PowerShell 占位符报 ParserError

不要在 PowerShell 中直接输入 `<host-id>`。尖括号会被当成重定向符号。

使用：

```powershell
YOUR_HOST_ID
```

或者让桌面入口自动读取本地配置，不再手动填写。

## RCON 密码缺失

错误示例：

```text
RCON password is required
```

含义：ACBH 需要 RCON 密码才能安全执行 `save-all flush` 并生成世界快照。

处理：在 `server.properties` 中确认：

```properties
enable-rcon=true
rcon.port=25575
rcon.password=请换成你自己的强密码
```

不要把 RCON 密码写进 PowerShell 命令历史或公开日志。优先使用环境变量或 GUI 密码输入框。
