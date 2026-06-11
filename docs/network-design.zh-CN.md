[English](./network-design.md) | [简体中文](./network-design.zh-CN.md)

# 网络设计

ACBH 的网络设计目标是降低公网 IPv4 成本，同时让本地 Host 承担真正的 Minecraft 运行负载。

## 设计原则

1. 公网节点轻量化；
2. 本地 Host 运行真实服务；
3. Player 通过稳定入口接入；
4. relay-only 路径先跑通；
5. direct / P2P 作为后续优化；
6. 所有二进制流量都当作 opaque bytes，不解析 Minecraft 协议。

## 公网节点职责

公网节点负责：

- Coordinator API；
- artifact sync；
- Host heartbeat；
- election 与 takeover；
- tunnel / player session 管理；
- relay fallback。

公网节点不直接运行主服。

## Relay-only 路径

relay-only 路径是 V1 的第一条完整网络路径。

```text
Minecraft Client
  -> Player local TCP proxy
  -> Public Node relay
  -> Host Agent relay client
  -> Host local Minecraft server or Velocity
```

Public Node relay 只做字节转发，不理解 Minecraft 协议。

## Host 侧

Host Agent 连接 Public Node 的 Host relay endpoint：

```text
/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/host
```

它携带：

```text
X-ACBH-Host-ID
X-ACBH-Host-Token
X-ACBH-Host-Generation
```

然后连接本地 target address。

如果本地入口是 Velocity：

```bash
--target-address 127.0.0.1:25577
```

如果是普通 Minecraft server：

```bash
--target-address 127.0.0.1:25565
```

## Player 侧

Player local proxy 监听本地地址，例如：

```bash
--listen-address 127.0.0.1:25565
```

Minecraft 客户端连接：

```text
127.0.0.1:25565
```

Player local proxy 连接 Public Node 的 Player relay endpoint：

```text
/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/player
```

它携带：

```text
X-ACBH-Player-ID
X-ACBH-Player-Token
```

## 端口独立性

Host target address 和 Player listen address 是独立的。

例子：Host 使用 Velocity：

```bash
acbh-agent relay host \
  --target-address 127.0.0.1:25577
```

Player 仍然可以监听本地 25565：

```bash
acbh-agent relay player \
  --listen-address 127.0.0.1:25565
```

玩家客户端连接：

```text
127.0.0.1:25565
```

也可以让 Player 本地监听 25577：

```bash
acbh-agent relay player \
  --listen-address 127.0.0.1:25577
```

此时玩家客户端连接：

```text
127.0.0.1:25577
```

## Direct / P2P

Direct / P2P 不是 PR29 的目标。

未来可以增加：

- direct candidate；
- NAT traversal；
- QUIC；
- WebRTC；
- relay fallback 策略；
- 传输层抽象。

但 V1 先保证 relay-only 可用。
