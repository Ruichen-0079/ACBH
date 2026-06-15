# 单公网服务器双 Velocity / 双 Fabric 部署指南（v0.1-demo）

> 本指南面向 v0.1-demo 演示与小规模自托管。它不是大型生产服高可用方案。
> 操作真实世界目录前，请先做独立备份，并先在复制出来的测试目录完成一次演练。

## 1. 这篇文档解决什么问题

本指南给出一条最短可验证路径：用一台低配置公网服务器承载两个玩家入口，
同机运行两套 Velocity + Fabric，并用 ACBH 做 artifact 同步、备份、恢复和故障接管。

- Velocity A + Fabric A 是主入口与主后端。
- Velocity B + Fabric B 是备用入口与备用后端。
- ACBH Coordinator 保存 group、Host、artifact、选举和接管状态。
- ACBH Dashboard 提供图形操作入口。
- ACBH Agent / Local Control 执行本机扫描、推送、拉取、恢复和进程控制。

v0.1-demo **不做玩家无感迁移**，也**不会自动修改 Velocity backend**。A 故障后，
运维者让 B 拉取并恢复最新 artifact、启动 Fabric B，再让玩家改连
`公网 IP:25575`。自动切换 Velocity backend 属于 v0.2。

## 2. 适用场景

适合：

- 小型朋友服或测试服。
- 长期低成本自托管。
- 服主机器和备用机器之间做 artifact 接管。
- 可以接受故障时短暂停服和玩家重连备用端口。
- 希望先验证同步、恢复、选举和接管闭环，再决定是否继续自动化。

不适合：

- 大型商业服。
- JVM 或在线玩家热迁移。
- 玩家无感切换。
- 多地区高可用。
- 复杂代理自动切流。
- 对零丢档有强承诺的生产服。

## 3. 总体架构

文字拓扑：

```text
玩家
├─ 正常：公网 IP:25565
│  └─ Velocity A 0.0.0.0:25565
│     └─ Fabric A 127.0.0.1:25566
└─ 故障/接管：公网 IP:25575
   └─ Velocity B 0.0.0.0:25575
      └─ Fabric B 127.0.0.1:25576

公网服务器
├─ ACBH Coordinator 127.0.0.1:6121
│  └─ Dashboard /dashboard
├─ Host A Local Control 127.0.0.1:6122
└─ Host B Local Control 127.0.0.1:6123（同时运行两个 Agent 时）
```

Coordinator 默认不要直接暴露公网。Dashboard 建议只通过本机、VPN，或带 TLS 和认证
的受保护反向代理访问。Local Control 必须保持 loopback-only。若只运行一个 Local
Control，可让 Host A 和 Host B 分时使用 `127.0.0.1:6122`；若同时运行，则为两个
Agent 使用不同的 `XDG_CONFIG_HOME`，并让 Host B 使用 `127.0.0.1:6123`。

## 4. 端口规划

| 端口 | 组件 | 监听建议 | 公网策略 |
|---|---|---|---|
| TCP 25565 | Velocity A 玩家入口 | `0.0.0.0` | 开放 |
| TCP 25566 | Fabric A 后端 | `127.0.0.1` | 不开放 |
| TCP 25575 | Velocity B 玩家备用入口 | `0.0.0.0` | 开放 |
| TCP 25576 | Fabric B 后端 | `127.0.0.1` | 不开放 |
| TCP 6121 | Coordinator / Dashboard | `127.0.0.1` | 仅本机、VPN 或受保护反代 |
| TCP 6122 | Host A Local Control | `127.0.0.1` | 不开放 |
| TCP 6123 | Host B Local Control（可选） | `127.0.0.1` | 不开放 |
| TCP 25567/25577 | RCON 示例端口 | `127.0.0.1` | 不开放 |

不要把 Fabric `25566/25576`、Local Control、RCON 直接暴露公网。Coordinator 若必须
跨公网访问，前面必须有 TLS、认证和防火墙；v0.1-demo 更推荐 VPN 或 SSH 隧道。

## 5. 推荐服务器配置

- `1C1G` 只适合极小测试，两个 JVM 同时运行时尤其容易内存不足。
- `2C2G` 更推荐；真实模组服应按模组数量、地图和在线人数继续增加内存。
- 磁盘至少 20 GB，长期服建议 40 GB 以上，并预留 artifact 与备份空间。
- Java 17。
- Node.js / pnpm 仅 Coordinator 构建和运行时依赖安装需要。
- Go 仅从源码构建 Agent 时需要；release 二进制可以直接运行。

## 6. 目录规划

系统服务建议：

```text
/opt/mc-stack/
├── velocity-a/
├── fabric-a/
├── velocity-b/
├── fabric-b/
├── acbh/
│   ├── coordinator/
│   ├── agent/
│   ├── config-a/
│   ├── config-b/
│   └── data/
└── backups/
```

普通用户测试可以使用：

```text
~/mc-stack/
├── velocity-a/
├── fabric-a/
├── velocity-b/
├── fabric-b/
├── acbh/
└── backups/
```

不要让 Fabric A 与 Fabric B 共用同一个 `serverDir`。恢复到 B 前必须停止 Fabric B，
并先保存 B 当前目录的独立备份。

## 7. 安装顺序

1. 准备公网服务器并更新系统。
2. 安装 Java 17。
3. 创建 `minecraft` 用户或普通运行用户。
4. 创建目录结构并设置最小权限。
5. 安装 Velocity A。
6. 安装 Velocity B。
7. 安装 Fabric A。
8. 安装 Fabric B。
9. 配置 Velocity A 指向 Fabric A。
10. 配置 Velocity B 指向 Fabric B。
11. 启动 Velocity/Fabric，先做纯 Minecraft 连通性测试。
12. 解包 ACBH v0.1-demo release artifact，并校验 `SHA256SUMS`。
13. 安装 Coordinator 运行依赖并启动 Coordinator。
14. 打开 Dashboard 图形控制面板。
15. 配置 Host A / Host B Agent 和 Local Control。
16. 对 Fabric A 执行 doctor、scan、validate、push。
17. 停止 Fabric B，在 Fabric B 上 pull/restore。
18. 启动 Fabric B。
19. 让玩家测试两个公网入口。
20. 做一次手动接管演练。
21. 最后复核 Velocity forwarding、backend 地址和公网绑定。

Fedora 安装基础依赖示例：

```bash
sudo dnf install -y java-17-openjdk-headless nodejs
sudo useradd --system --create-home --shell /sbin/nologin minecraft
sudo install -d -o minecraft -g minecraft \
  /opt/mc-stack/{velocity-a,fabric-a,velocity-b,fabric-b,acbh,backups}
```

Ubuntu/Debian 可使用 `openjdk-17-jre-headless`。具体 Velocity/Fabric 安装方式请以它们
各自的官方文档为准。

## 8. Velocity 配置

仓库提供两个最小片段：

- [velocity-a.toml.example](../../examples/single-vps-dual-stack/velocity-a.toml.example)
- [velocity-b.toml.example](../../examples/single-vps-dual-stack/velocity-b.toml.example)

核心关系是：

```toml
# Velocity A
bind = "0.0.0.0:25565"
[servers]
main = "127.0.0.1:25566"
```

```toml
# Velocity B
bind = "0.0.0.0:25575"
[servers]
main = "127.0.0.1:25576"
```

这些只是最小片段，不是完整 `velocity.toml`。必须按实际认证方案设置
player-info forwarding、modern forwarding secret、forced-hosts 等选项。不要复用网上的
真实 secret，也不要把 forwarding secret 提交到仓库。

## 9. Fabric `server.properties`

示例：

- [Fabric A server.properties](../../examples/single-vps-dual-stack/fabric-a-server.properties.example)
- [Fabric B server.properties](../../examples/single-vps-dual-stack/fabric-b-server.properties.example)

示例使用：

- Fabric A：游戏端口 `25566`，RCON `25567`。
- Fabric B：游戏端口 `25576`，RCON `25577`。
- `server-ip=127.0.0.1`，避免后端直接公网监听。
- `rcon.password=CHANGE_ME` 只是占位，部署时必须替换，且不能写入版本库或进程参数。

本指南不强行固定 `online-mode`：

- 走 Velocity 正版验证/转发时，应按 Velocity + FabricProxy-Lite 或选定 forwarding
  方案完整配置。
- 测试环境临时使用离线模式时，必须了解身份伪造风险。
- v0.1-demo 不鼓励把离线模式 Fabric 直接裸露到公网。

## 10. ACBH Agent 示例

示例文件：

- [Host A](../../examples/single-vps-dual-stack/acbh-agent-a.example.json)
- [Host B](../../examples/single-vps-dual-stack/acbh-agent-b.example.json)

两个示例包含任务规划所需的 placeholder，包括 `example-group`、
`example-host-token`、`example-access-key` 和 `example-local-control-token`。它们不是
有效凭据，不能用于真实部署。

当前 Agent 实际运行配置由 `login` 生成。示例中的 `accessKey` 与
`localControlToken` 是部署清单占位字段：真实 access key 只在登录时提供；Local
Control token 由 `control serve` 自动生成并保存在各自配置目录的
`acbh/control-token`，不应复制进 Agent 配置。

同机注册两个 Host 时，必须隔离配置目录。以下做法不会把 access key 放入 argv 或
shell 历史：

```bash
# Host A
export XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-a
read -rsp "Group access key: " ACBH_ACCESS_KEY
echo
export ACBH_ACCESS_KEY
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 login \
  --coordinator http://127.0.0.1:6121 \
  --group-id example-group \
  --name "Public Fabric A" \
  --device-name single-vps-fabric-a \
  --platform linux
unset ACBH_ACCESS_KEY

# Host B：换用 config-b，并重复受保护输入流程
export XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-b
```

真实 `groupId` 由 Dashboard 创建 group 后显示。不要照抄 `example-group`。

分别启动 Local Control：

```bash
XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-a \
  /opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 control serve \
  --listen 127.0.0.1:6122

XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-b \
  /opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 control serve \
  --listen 127.0.0.1:6123
```

不要使用 `--token` 把真实 token 放入 argv；让 Agent 自动生成 token 文件。

## 11. systemd 示例

模板位于 [examples/single-vps-dual-stack/systemd](../../examples/single-vps-dual-stack/systemd/)：

- `velocity-a.service.example`
- `velocity-b.service.example`
- `fabric-a.service.example`
- `fabric-b.service.example`
- `acbh-coordinator.service.example`

它们统一使用 `User=minecraft` 和 `/opt/mc-stack/...` 示例路径。复制到
`/etc/systemd/system/` 前，必须按实际 jar 名称、内存和路径修改。模板不包含真实用户
名或 secret。

release bundle 中 Coordinator 文件位于 `coordinator/`。首次启动前安装生产依赖：

```bash
cd /opt/mc-stack/acbh/coordinator
corepack enable
pnpm install --prod
sudo systemctl daemon-reload
sudo systemctl enable --now acbh-coordinator
```

检查：

```bash
systemctl status acbh-coordinator --no-pager
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:6121/health
```

## 12. 用 Dashboard 图形控制面板完成接管演练

### A. 打开 Dashboard

Coordinator 启动后访问 `http://127.0.0.1:6121/dashboard`。推荐只在本机、VPN 或受保护
反代中访问，不要直接裸露公网。

从远程电脑访问时，可同时转发 Dashboard 和两个 Local Control 端口：

```bash
ssh -L 6121:127.0.0.1:6121 \
    -L 6122:127.0.0.1:6122 \
    -L 6123:127.0.0.1:6123 \
    minecraft@PUBLIC_IP
```

然后在本地浏览器打开 `http://127.0.0.1:6121/dashboard`。

### B. 连接 Local Control

1. 在 Dashboard 的 Agent 区输入 `http://127.0.0.1:6122` 或 `:6123`。
2. 从对应配置目录的 `acbh/control-token` 读取 token，手动填入密码框。
3. 点击“连接本机 Agent”。

token 只在页面内存中保存，不进入 localStorage、sessionStorage、URL 或 console。
页面刷新后应重新输入；401/403 后 Dashboard 会清理内存中的 token。非 loopback 地址会
显示风险警告，不建议继续。

### C. Fabric A 初次同步

1. 打开 Dashboard。
2. 连接 Coordinator，确认 `/health` 正常。
3. 选择或创建 group，并妥善保管一次性 access key。
4. 注册 Host A，确认 Host ID 和 heartbeat。
5. 进入 Agent，连接 Host A 的 Local Control。
6. 将服务端目录设为 `/opt/mc-stack/fabric-a`，执行 **doctor**。
7. 执行 **scan**；真实世界一致性快照优先使用 **safe-sync world**。
8. 对生成的文件执行 **Validate manifest**。
9. 执行 **push world** 或对应 artifact push。
10. 在 Artifacts 区检查 **latest artifact**。

### D. Fabric B 恢复

1. 切换到 Host B / `fabric-b` 的 group 和 Host 信息。
2. 将 Local Control 地址改为 `http://127.0.0.1:6123` 并连接 Host B。
3. 确认 Fabric B 已停止，再执行 **pull world**，artifact ID 使用 `latest`。
4. pull 会把 manifest 内容恢复到指定 output 目录；确认目标是
   `/opt/mc-stack/fabric-b`，危险删除选项只在明确核对后使用。
5. 执行 **server status**。
6. 确认启动 jar 和结构化 JVM/server 参数后执行 **server start**。
7. 通过 Velocity B 的 `公网 IP:25575` 测试进入。

### E. 故障演练

1. 停止 Fabric A，或在受控测试中停止 Host A 心跳。
2. 在 Dashboard 查看 Host 状态变化。
3. 点击 **Run election**，或等待超时后点击 **Check timeout**。
4. 在 Election / Takeover 区点击 **Poll assignment**。
5. 切到 Host B，确认 assignment 后点击 **Accept takeover**。
6. 在 Host B 完成 pull/restore/start；也可在 CLI 使用 `takeover run`。
7. 确认服务可用后点击 **Complete takeover**。
8. 让玩家连接 `公网 IP:25575` 验证。

这是故障接管，不是普通重启。执行前必须确认原 Host 不再写同一份世界数据。

### F. Dashboard 可见状态

Dashboard 应能显示或刷新：

- Coordinator 状态。
- Group 状态。
- current host。
- current host generation。
- candidate / election / takeover 状态。
- latest artifact。
- server status。
- 当前页面的 event log（敏感字段会遮蔽）。

### G. Dashboard 不做的事

v0.1-demo Dashboard 不承诺：

- 自动修改 Velocity A/B 配置。
- 自动迁移在线玩家。
- 自动公网 DNS 切换。
- 自动创建公网隧道。
- 大型服无损热迁移。

## 13. 手动命令行接管流程

命令与参数以当前 `acbh-agent --help` 为准。下面只使用 v0.1-demo 已存在的命令。

Host A 生成一致性快照并推送：

```bash
export XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-a
read -rsp "RCON password: " ACBH_RCON_PASSWORD
echo
export ACBH_RCON_PASSWORD

/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 doctor
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 safe-sync \
  --server-dir /opt/mc-stack/fabric-a \
  --rcon-host 127.0.0.1 \
  --rcon-port 25567 \
  --artifact-id world-manual-001 \
  --group-id example-group \
  --creator-host-id public-fabric-a \
  --server-pack-version demo \
  --output /opt/mc-stack/backups/world-manual-001.manifest.json
unset ACBH_RCON_PASSWORD

/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 manifest validate \
  --file /opt/mc-stack/backups/world-manual-001.manifest.json
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 push \
  --manifest /opt/mc-stack/backups/world-manual-001.manifest.json \
  --server-dir /opt/mc-stack/fabric-a
```

`example-group` 和 `public-fabric-a` 必须换成登录后实际值。不要把真实 RCON 密码放在
`--rcon-password` 参数中。

Host B 拉取、恢复和启动：

```bash
export XDG_CONFIG_HOME=/opt/mc-stack/acbh/config-b
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 server status
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 server stop
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 pull \
  --artifact-kind world-snapshot \
  --artifact-id latest \
  --output-dir /opt/mc-stack/fabric-b
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 server start
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 server status
```

选举与接管：

```bash
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 election status
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 election check-timeout
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 takeover poll
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 takeover accept
# 完成 pull/restore/start 并验证后：
/opt/mc-stack/acbh/agent/acbh-agent-linux-amd64 takeover complete
```

CLI 没有 `election run` 子命令；手动 Run election 由 Dashboard 提供。也可用
`takeover run --dry-run` 先查看自动执行计划，再决定是否执行。

## 14. 打通公网流程

### A. 防火墙 / 安全组

只开放：

- TCP 25565。
- TCP 25575。

不要开放：

- TCP 25566、25576。
- TCP 6122、6123。
- RCON。
- 直接裸露的 TCP 6121。

Fedora firewalld 示例：

```bash
sudo firewall-cmd --permanent --add-port=25565/tcp
sudo firewall-cmd --permanent --add-port=25575/tcp
sudo firewall-cmd --reload
```

云厂商安全组也必须只添加两个玩家入口。

### B. 本机监听检查

```bash
ss -ltnp | grep -E '25565|25566|25575|25576|6121|6122|6123'
```

确认 Velocity 监听公网地址，Fabric、Coordinator 和 Local Control 只监听 loopback。

### C. 外部连通性检查

从另一台机器或本地网络外：

- Minecraft 客户端连接 `公网 IP:25565`。
- Minecraft 客户端连接 `公网 IP:25575`。

### D. 如果连不上

依次检查：

- 云厂商安全组。
- Linux 防火墙。
- Velocity 是否绑定 `0.0.0.0`。
- Fabric 是否绑定 `127.0.0.1`。
- Velocity backend 地址是否写错。
- 端口是否冲突。
- Velocity/Fabric forwarding 配置是否匹配。

## 15. 常见问题

### 端口被占用

用 `ss -ltnp` 找到占用进程。只停止本部署对应的 systemd 单元，不要按进程名批量杀死
系统服务。

### Velocity 能启动但玩家进不去

检查公网安全组、firewalld、`bind`、Velocity 日志和客户端版本。

### Velocity 能进但转发到 Fabric 失败

检查 backend 地址、Fabric 是否监听、forwarding 模式和 FabricProxy-Lite 配置。

### Fabric 端口是否应该公网开放

不应该。Velocity 与 Fabric 同机时，后端保持 loopback。

### Dashboard 打不开

先检查 `curl http://127.0.0.1:6121/health`，再检查 SSH/VPN/反代。不要为了方便把
Coordinator 直接改成裸露公网。

### Local Control unavailable

检查对应 Agent 的 `XDG_CONFIG_HOME`、监听端口、进程日志和 SSH 端口转发。Host A/B
不能同时抢占 `6122`。

### token rejected

从当前 Host 对应的 `acbh/control-token` 重新读取。不要使用另一个 Host 的 token；
401/403 后刷新输入，禁止把 token 放进 URL。

### pull/restore 后世界没变

确认拉取的是 `world-snapshot` latest、output 目录正确、Fabric B 在恢复期间已停止，并
检查 manifest 中世界文件路径。先在复制目录验证，不要反复覆盖真实世界。

### RCON 失败

检查 `enable-rcon`、RCON 端口、loopback 监听、密码来源和服务日志。密码应通过受保护的
环境输入，不进入 argv。

### Windows 路径问题

Windows 使用原生绝对路径和 release 的 `.exe`。不要把 file URL 的 `/C:/...` 当成本地
路径；避免 junction/symlink 作为恢复目录。

### demo-smoke 和真实部署有什么区别

`demo-smoke.sh` 使用临时目录和假世界文件，不启动真实 Minecraft、不访问公网。它证明
API 与 artifact 主闭环，不证明你的 Velocity、Fabric、RCON 或防火墙配置正确。

### 如何回滚到 Fabric A

停止 B 的写入，必要时从 B 生成最后一个安全快照；让 A 重新成为接管目标，恢复并启动
Fabric A，验证后让玩家回到 `25565`。不要让 A/B 同时写同一个世界副本。

### 如何清理残留进程

优先使用 `systemctl stop <unit>`。Agent 管理的服务器先运行 `server status` 和
`server stop`。只有确认记录 PID 已死亡时才使用 `server repair-state`，不要静默删除
未知 state 后直接重启。

### 何时需要 v0.2 自动切流

当你需要固定单入口、自动改 Velocity backend、自动迁移玩家或 DNS/代理联动时，就超出
v0.1-demo 双端口手动/半自动接管范围。

## 16. 最终验收清单

- [ ] Velocity A 启动。
- [ ] Fabric A 启动。
- [ ] Velocity B 启动。
- [ ] Fabric B 启动。
- [ ] 玩家能进入 25565。
- [ ] 玩家能进入 25575。
- [ ] Coordinator 启动。
- [ ] Dashboard 能打开。
- [ ] Dashboard 能连接 Local Control。
- [ ] Agent A doctor/scan/validate/push 成功。
- [ ] Agent B pull/restore/start 成功。
- [ ] latest artifact 可见。
- [ ] takeover complete 可执行。
- [ ] `verify-all` PASS。
- [ ] `demo-smoke` PASS。
- [ ] `build-v0.1-demo` PASS。
- [ ] `SHA256SUMS` 校验通过。
- [ ] `dist` 产物未提交。
- [ ] 示例文件无真实 secret。

## 17. v0.2 MVP 的完整目录 bootstrap

v0.2 MVP 推荐用
[`server-runtime` bootstrap](server-runtime-bootstrap.md)
在 Host A 与 Host B 之间同步完整可运行的 `serverDir`。第一个 Host 使用
`bootstrap create-group` 创建 group、首次 push 并确认 current host；其他 Host
使用 `bootstrap join-group` 拉取 latest、restore 并逐文件校验。

v0.1-demo 的 `scan`、`safe-sync`、`push`、`pull` 手动 artifact 流程仍保持兼容，
适合分类型快照和排障。`server-runtime` 只是把首次完整目录复制缩短为两个明确命令，
不会自动切换 Velocity，也不会让新加入 Host 自动成为 current host。
