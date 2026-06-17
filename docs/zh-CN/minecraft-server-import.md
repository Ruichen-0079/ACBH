# Minecraft 服务端目录导入

私人桌面版会逐步提供图形化导入向导。当前版本先约定导入校验规则，桌面入口和后续 GUI 都按这些规则实现。

## 选择目录

选择 Minecraft 服务端根目录，也就是包含 `server.properties`、`world\`、`mods\` 等文件的目录。

常见可识别文件：

- `fabric-server-launch.jar`
- `server.jar`
- `paper.jar`
- `velocity.jar`
- `mods\`
- `config\`
- `world\`
- `server.properties`
- `eula.txt`

## 服务端类型识别

ACBH 按以下优先级识别：

- 存在 `fabric-server-launch.jar`：Fabric
- 存在 `paper.jar`：Paper
- 存在 `velocity.jar`：Velocity
- 存在 `server.jar`：Vanilla
- 都不存在：Unknown

## 启动命令建议

示例：

```powershell
java -Xms2G -Xmx4G -jar fabric-server-launch.jar nogui
```

后续 GUI 会允许修改 `Xms` 和 `Xmx`。不要把 RCON 密码放进启动命令。

## EULA

如果 `eula.txt` 不存在，ACBH 只能提示你阅读并确认 Minecraft EULA。只有你明确确认后，程序才可以写入：

```text
eula=true
```

## RCON 检查

ACBH 会检查 `server.properties` 中的：

- `server-port`
- `enable-rcon`
- `rcon.port`
- `rcon.password`

如果 RCON 未开启，会提示你写入建议配置，但不会自动覆盖 `server.properties`。只有点击确认后才会修改。

建议配置：

```properties
enable-rcon=true
rcon.port=25575
rcon.password=请换成你自己的强密码
```

## 第一次世界快照

1. 先确认桌面入口启动成功，并且心跳已发送。
2. 导入服务端目录。
3. 确认 RCON 已开启且密码正确。
4. 执行 `safe-sync world-snapshot`。

`safe-sync` 会先通过 RCON 执行保存，再扫描目录生成世界快照。RCON 密码不会打印到日志。

## 后续 scan/safe-sync/push

导入完成后，`serverDir`、启动命令、日志目录和停止超时会保存到本地配置。后续 `scan`、`safe-sync`、`push` 默认使用已导入的目录，不再要求普通用户反复手写路径。
