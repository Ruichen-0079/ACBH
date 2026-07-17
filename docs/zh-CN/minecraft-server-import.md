# Minecraft 服务端目录导入

v0.3.1 的 WinForms GUI 已接通导入、启动、扫描、安全同步和上传闭环。

## 选择目录

点击“导入 MC 服务端目录”，选择 Minecraft 服务端根目录，也就是包含这些文件或目录的位置：

- `server.properties`
- `world\`
- `mods\`
- `config\`
- `eula.txt`
- `fabric-server-launch.jar`、`paper.jar`、`server.jar` 或 `velocity.jar`

路径可以包含空格。保存导入配置后，ACBH 会把 `serverDir` 和建议启动命令写入本地 `config.yaml`。

## 服务端类型识别

ACBH 按以下优先级识别：

- 存在 `fabric-server-launch.jar`：Fabric
- 存在 `paper.jar`：Paper
- 存在 `velocity.jar`：Velocity
- 存在 `server.jar`：Vanilla
- 都不存在：Unknown

## 启动和停止

GUI 的“启动 MC 服务端”会使用导入时保存的 command。日志写入 ACBH 日志目录，进程状态由 ACBH runtime state 管理。

GUI 的“停止 MC 服务端”只停止由 ACBH 启动的服务端，不会误杀用户自己启动的 Java 进程。

## RCON 检查

点击“检查远程控制 RCON”会检查：

- `server.properties` 是否存在
- `enable-rcon=true`
- `rcon.port` 是否存在
- `rcon.password` 是否已设置

ACBH 只显示“是否已设置密码”，不会输出密码本身。

建议配置：

```properties
enable-rcon=true
rcon.port=25575
rcon.password=请换成你自己的强密码
```

## safe-sync 世界快照

点击“安全同步世界快照”后：

1. GUI 先检查 RCON。
2. 如果 RCON 未开启或密码未配置，直接显示中文修复建议，不进入密码输入。
3. 如果 RCON 已配置，GUI 通过隐藏输入框读取密码。
4. ACBH 通过 RCON 执行 `save-all flush`。
5. ACBH 生成 `world-YYYYMMDD-HHMMSS.manifest.json`。

RCON 密码不会出现在 GUI 文本框、日志或命令行参数中。

## scan 服务端包

点击“扫描服务端包 scan server-pack”会生成：

```text
%APPDATA%\ACBH\manifests\server-pack-YYYYMMDD-HHMMSS.manifest.json
```

成功后 GUI 会显示制品类型、Artifact ID、manifest 路径、included files、ignored files 和 total bytes。

## push 上传

点击“上传同步制品 push”会默认上传最近一次成功生成的 manifest。

上传前必须满足：

- 本机 Host 是 current host。
- manifest 的 `groupId` 和 `creatorHostId` 与本地配置匹配。
- 本地文件与 manifest 中的 SHA256 一致。

如果不是 current host，GUI 会提前阻断并提示中文原因。

## pull 拉取

点击“拉取同步制品 pull”会提示：

```text
pull 可能覆盖本地服务端文件，请确认已备份。
```

默认拉取最新 `world-snapshot`，输出目录是已导入的 `serverDir`，默认不应用删除项。
