# server-runtime 主机引导

`server-runtime` 是 ACBH v0.2 MVP 用来同步完整、可直接启动的 Minecraft
服务端目录的 artifact 类型。它适合把 Host A 的 Fabric `serverDir` 复制到
Host B，而不只同步世界或模组包的一部分。

## 与现有 artifact 的区别

| 类型 | 用途 |
|---|---|
| `world-snapshot` | 世界与插件运行时数据；适合频繁安全快照 |
| `server-pack` | jar、mods、config、libraries 等启动文件 |
| `admin-state` | `server.properties`、白名单、OP、封禁列表等管理状态 |
| `server-runtime` | 上述内容与其他必要文件组成的完整可运行 `serverDir` |

`server-runtime` 不替代运行中的一致性快照。真实服务器在扫描前仍应停止写入，
或先通过 RCON 执行 `save-all flush`。不要让两个 Host 同时写同一个世界副本。

## 默认排除

扫描采用“完整目录加默认排除”规则，不要求文件必须位于 Minecraft 专用目录。
默认排除：

- 目录：`logs/`、`crash-reports/`、`cache/`、`.cache/`、`tmp/`、
  `temp/`、`dist/`、`node_modules/`、`.git/`
- 文件：`session.lock`、`.DS_Store`、`Thumbs.db`
- 后缀：`*.log`、`*.tmp`
- symlink、junction、reparse point 不进入 manifest

本地 profile 会记录这些规则。Coordinator 不保存或决定 Host 的绝对路径。

## 第一个 Host：create-group

```bash
export XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-a

acbh-agent bootstrap create-group \
  --coordinator http://127.0.0.1:6121 \
  --group "example-group" \
  --server-dir /opt/mc-stack/fabric-a \
  --artifact-class server-runtime \
  --name "Fabric Host A" \
  --device-name fabric-a
```

该命令依次完成：

1. 在调用 Coordinator 前预检并扫描本地 `serverDir`，本地输入无效时不创建 group。
2. 创建 group 并注册 Host A；凭据先保存到权限受限的 recovery 目录。
3. 扫描完整 `serverDir`，生成带 generation、group ID、host ID、文件大小、
   mtime 和 SHA-256 的 `server-runtime` manifest。
4. 发送 heartbeat，并通过现有 `no-current-host` election/takeover 流程将
   Host A 确认为 current host。
5. 将 host token 保存到正式 Agent config，并将一次性 group access key 保存到本地
   `group-access-key` 文件；输出只包含文件路径。
6. 使用新的 current host generation 上传对象并原子提交 manifest；只有提交成功后
   latest 才会更新。
7. 发送包含 latest `server-runtime` 的 hosting heartbeat，并清理 recovery 目录。

已有本地 profile 时命令默认拒绝覆盖。只有明确确认要替换本机身份时才使用
`--force`，旧 profile 在新 bootstrap 通过本地预检前不会被替换。远端注册后的失败会
返回 recovery 目录路径，不在错误中打印凭据。current host 建立后若上传失败，latest
不会更新；正式 profile 和 recovery manifest 可用于重新执行 artifact push。对象上传
中断时可能留下可复用或由 GC 清理的 content-addressed 对象。

## 其他 Host：join-group

把 Host A 保存的 group access key 通过可信渠道交给 Host B。不要把 access key
写进命令参数、URL、日志或聊天记录。

```bash
export XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-b
read -rsp "Group access key: " ACBH_ACCESS_KEY
echo
export ACBH_ACCESS_KEY

acbh-agent bootstrap join-group \
  --coordinator http://127.0.0.1:6121 \
  --group <group-id> \
  --server-dir /opt/mc-stack/fabric-b \
  --artifact-class server-runtime \
  --name "Fabric Host B" \
  --device-name fabric-b

unset ACBH_ACCESS_KEY
```

该命令注册 Host B、查询 latest `server-runtime`、恢复所有 manifest 文件并逐个
校验大小和 SHA-256。成功输出 `Verify result: PASS`。

目标目录不存在时会安全创建。目标目录非空时默认拒绝；确认允许覆盖目录内文件时
显式增加 `--allow-non-empty`。该参数不会放宽 path escape、symlink、junction、
Windows ADS 或保留设备名检查。

Host B 加入后只是 standby/candidate，不会自动成为 current host。故障接管仍需
显式 election、takeover accept、恢复/启动验证和 takeover complete。

## “100% 文件同步”的边界

这里的 100% 表示：

- manifest 中每个非删除文件都已恢复；
- 每个恢复文件的大小与 SHA-256 都与 manifest 一致；
- 缺失文件或 hash mismatch 必须失败；
- 默认排除或本地排除规则未纳入 manifest 的文件不要求一致；
- 空目录不进入 manifest，也不会在 Host B 自动重建；需要空目录的模组应在启动时创建，
  或由运维者在 restore 后显式创建；
- restore 后目录具备作为 Fabric `serverDir` 启动的文件基础。

它不代表 JVM 内存、在线玩家会话、尚未落盘数据或两个运行中世界的实时一致性。

## Restore 安全

`server-runtime` 复用现有 hardened restore：

- 拒绝绝对路径、`../`、Windows drive/UNC 路径和 NUL；
- 逐级检查父目录，不跟随 symlink、junction 或 reparse point；
- 拒绝最终目标 symlink、Windows ADS 和保留设备名；
- 删除操作不得逃出 `serverDir`；
- 下载对象在临时文件中校验并原子替换目标。

## Dashboard

1. 在 Artifacts 区选择 `server-runtime`，点击“最新制品”查看 artifact ID、
   generation、文件数、总字节数和 creator host；响应中的 `latest: true`
   表明这是该类别的 latest。
2. 连接 loopback-only Local Control。
3. 点击“扫描 server-runtime”，再执行 Validate manifest。
4. 点击 `push server-runtime`。
5. 在 Host B 连接对应 Local Control 后点击
   `pull latest server-runtime + verify`。

Dashboard 不提供文件选择器；路径来自 Agent 配置或页面输入。secret 仍只保存在
页面内存，不进入 localStorage、sessionStorage、URL 或 console。
Dashboard restore 会先确认危险操作，再单独询问是否允许非空目录。未明确允许时，
Local Control 对非空 `server-runtime` 目标返回 `restore_target_not_empty`，不会开始下载
或覆盖。

## 常见失败

- `local host profile already exists`：确认当前 `XDG_CONFIG_HOME` 是否属于该 Host；
  只有明确替换身份时使用 `--force`。
- `latest server-runtime artifact is unavailable`：Host A 尚未成功 push，或选择了错误 group。
- `restore directory is not empty`：使用空目录，或备份后显式使用 `--allow-non-empty`。
- `generation is stale` / `not the current host`：刷新 group/election 状态，不要绕过 fencing。
- `file is missing` / `sha256 mismatch`：恢复不完整或文件被本地程序修改；停止服务后重试。
- symlink/junction/reparse point 错误：移除危险路径布局，不要降低 restore 检查。

## 当前不支持

- 自动修改 Velocity backend；
- DNS 自动切换；
- 在线玩家无感迁移；
- JVM 热迁移；
- 实时双写、P2P 或增量同步优化。
