[English](./README.md) | [简体中文](./README.zh-CN.md)

# ACBH — Anyone Can Be Host

ACBH 是一个面向 Minecraft 群服的分布式 Host 接管与中继平台。

它的目标不是让昂贵的公网服务器长期运行 Minecraft 主服，而是让低成本公网 IPv4 节点承担控制、同步和中继职责，让真正的 Minecraft 服务运行在可信玩家的本地 Host 设备上。

V1 不是无缝热迁移。它采用文件级快照同步、快速 Host 选举、接管恢复和 relay fallback，让群服在当前 Host 离线后尽快由另一台可信设备恢复服务。

## V1 承诺

```text
Host A 离线。
Coordinator 选举 Host B。
Host B 恢复最新已验证快照。
Host B 启动 Minecraft 服务端或 Velocity。
玩家重新连接。
```

目标 V1 恢复时间：**10–30 秒**。

## 非目标

- 不做 JVM 内存热迁移。
- 不迁移 tick、实体、红石、mod runtime 状态。
- 不透明迁移玩家在线会话。
- 不要求 Minecraft mod 或 plugin。
- 不把公网 IPv4 节点当作主服本体。

## 当前产品方向

ACBH V1 中，公网节点是轻量控制与中转节点，主要负责：

1. Coordinator 控制面；
2. artifact 同步与中转；
3. Host / Player 会话协调；
4. relay fallback；
5. Host 选举与接管协调。

真正运行 Minecraft Server、Fabric、Paper 或 Velocity 的，是本地 Host。

## 仓库结构

```text
ACBH/
├── apps/
│   └── coordinator/      # TypeScript Coordinator service
├── agent/                # Go cross-platform Agent CLI
├── docs/                 # Architecture and protocol documents
├── examples/             # Local demo and deployment examples
└── .github/workflows/    # CI
```

## 主要组件

### Coordinator

Coordinator 是运行在公网节点上的轻量服务。它负责 group、member、host、heartbeat、snapshot、storage metadata、host election、takeover assignment、tunnel session 和 relay 控制面。

Coordinator 不运行 Minecraft。

### Agent

Agent 是安装在候选 Host 设备上的本地 CLI / daemon。它负责下载 server pack、启动 Minecraft、执行 safe sync、上传 snapshot、上报健康状态、参与接管，并作为 Host relay client 把公网 relay 的二进制流转发到本地 Minecraft 服务端或 Velocity。

### Storage

Storage 是面向 server pack、snapshot manifest 和 file blob 的内容寻址文件存储。V1 从本地文件系统存储开始，之后可加入 S3 兼容存储。

### Public Relay

Public Relay 运行在公网节点上。它接收 Host 侧和 Player 侧 WebSocket 连接，并在两端配对后转发二进制帧。

Relay 不解析 Minecraft 协议，只转发 opaque bytes。

## Relay-only 链路

当前 Host 侧已经具备：

```text
Public Node relay
  <-> Host Agent relay client
  <-> Host local Minecraft server or Velocity
```

PR29 将补齐 Player 侧：

```text
Minecraft Client
  <-> Player local TCP proxy
  <-> Public Node relay
  <-> Host Agent relay client
  <-> Host local Minecraft server or Velocity
```

## 端口说明

ACBH 不应硬编码 Minecraft 端口。

有两个独立地址：

### Host target address

这是 Host Agent 转发到本地真实服务入口的地址。

如果 Host 本地入口是 Velocity：

```bash
acbh-agent relay host --target-address 127.0.0.1:25577
```

如果 Host 本地入口是普通 Minecraft 服务端：

```bash
acbh-agent relay host --target-address 127.0.0.1:25565
```

### Player listen address

这是玩家 Minecraft 客户端连接的本地代理地址。

默认可以是：

```bash
acbh-agent relay player --listen-address 127.0.0.1:25565
```

玩家客户端连接：

```text
127.0.0.1:25565
```

也可以设置为：

```bash
acbh-agent relay player --listen-address 127.0.0.1:25577
```

玩家客户端连接：

```text
127.0.0.1:25577
```

Host target address 和 Player listen address 互相独立。Host 使用 Velocity 25577，不代表 Player 本地也必须监听 25577。

## 常用开发命令

Coordinator：

```bash
pnpm build:coordinator
pnpm --filter @acbh/coordinator test
```

Agent：

```bash
cd agent
go test ./... -count=1
go vet ./...
```

完整检查：

```bash
pnpm build:coordinator
pnpm --filter @acbh/coordinator test
cd agent && go test ./... -count=1
cd agent && go vet ./...
```

## 中文文档

- [文档索引](./docs/README.zh-CN.md)
- [V1 架构](./docs/v1-architecture.zh-CN.md)
- [网络设计](./docs/network-design.zh-CN.md)
- [隧道协议](./docs/tunnel-protocol.zh-CN.md)
- [选举设计](./docs/election-design.zh-CN.md)
- [安全说明](./docs/security.zh-CN.md)
- [Codex 开发指南](./docs/codex-guide.zh-CN.md)

## v0.1-demo

当前发布分支：`release/v0.1-demo-prep`

- [Release notes](docs/release-notes-v0.1-demo.md)（英文）— 已完成能力、安全默认值、已知限制、平台验证
- CLI demo：`bash scripts/demo-smoke.sh`
- GUI demo：`pnpm dev:coordinator` → `http://127.0.0.1:6121/dashboard`
- [Release checklist](docs/release-checklist.md)（英文）— 9 部分发布前验证清单
- Go tests：14 包全部通过，Coordinator tests：123/123
- 已在 Fedora 41 和 Windows 11 (PowerShell) 上验证

**不适用于生产环境。** 默认仅本地回环，内存存储，无 TLS。
仅限本地开发使用。
