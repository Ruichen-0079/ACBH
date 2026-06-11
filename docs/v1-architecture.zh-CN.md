[English](./v1-architecture.md) | [简体中文](./v1-architecture.zh-CN.md)

# ACBH V1 架构

ACBH V1 的目标是让 Minecraft 群服可以在低成本公网节点和本地 Host 之间协作运行。

公网 IPv4 节点不直接运行 Minecraft 主服。它主要负责控制面、同步制品存储、会话协调以及必要时的 relay fallback。真正的 Minecraft 服务端运行在本地 Host 上。

## 核心组件

### Coordinator

Coordinator 运行在公网节点上，负责：

- 组管理；
- Host 注册；
- Host 心跳；
- 当前 Host 状态；
- 选举和接管状态；
- artifact 元数据；
- latest artifact 指针；
- tunnel session / player session 管理；
- relay 控制面；
- 可选的本地 JSON 状态持久化。

### Agent

Agent 运行在 Host 本地机器上，负责：

- 扫描服务端目录；
- 生成 manifest；
- 校验 manifest；
- push / pull artifacts；
- RCON safe-sync；
- 服务端 start / stop / status；
- 发送 heartbeat；
- 参与 election 和 takeover；
- 连接 Public Relay 的 Host endpoint；
- 将 relay WebSocket 二进制帧转发到本地 Minecraft server 或 Velocity。

### Public Node

Public Node 是公网 IPv4 节点，通常运行 Coordinator，也可以承担 artifact 中转和 relay fallback。

它不是主服本体。

### Player local proxy

Player local proxy 是玩家本地代理。它监听本地地址，例如 `127.0.0.1:25565`。玩家 Minecraft 客户端连接这个本地代理，代理再连接 Public Node 的 Player relay endpoint。

PR29 会补齐该部分。

## 同步制品

ACBH V1 使用三类主要 artifact：

### server-pack

服务端包，通常包含 mods、plugins、配置、启动脚本和其他非世界状态文件。

### world-snapshot

世界快照，通常包含 world、region、playerdata 和其他世界状态。

### admin-state

管理状态，通常包含 whitelist、ops、ban 列表、权限数据和其他管理文件。

推荐恢复顺序：

```text
server-pack -> admin-state -> world-snapshot
```

## Host 选举与接管

当当前 Host 离线或超时后，Coordinator 可以发起选举。

候选 Host 会根据自身条件产生 Host Score。Coordinator 选出合适的 Host 后，进入 takeover assignment 流程。

接管流程使用一次性 takeover token。Coordinator 只应保存 token 的 hash/verifier，不应持久化 raw token。

接管完成后才递增 `currentHostGeneration`。

## Generation

`currentHostGeneration` 用来防止 stale Host 写入或继续服务。

Host 上传 artifact 或接入 relay 时必须携带正确 generation。过期 generation 的 Host 应被拒绝。

## Relay 架构

PR27 引入 Public Relay MVP。

PR28 引入 Host Agent relay client。

当前 Host 侧链路：

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

## 端口模型

ACBH 不应假设所有服务都在 25565。

Host target address 是 Host Agent 转发到本地真实入口的地址。例如 Velocity：

```bash
--target-address 127.0.0.1:25577
```

Player listen address 是玩家本地客户端连接的地址。例如：

```bash
--listen-address 127.0.0.1:25565
```

这两个地址彼此独立。
