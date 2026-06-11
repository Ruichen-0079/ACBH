[English](./README.md) | [简体中文](./README.zh-CN.md)

# ACBH 文档索引

这里收录 ACBH 的主要架构、协议、安全和开发文档。

## 中文文档

- [V1 架构](./v1-architecture.zh-CN.md)
- [网络设计](./network-design.zh-CN.md)
- [隧道协议](./tunnel-protocol.zh-CN.md)
- [选举设计](./election-design.zh-CN.md)
- [安全说明](./security.zh-CN.md)
- [Codex 开发指南](./codex-guide.zh-CN.md)

## 英文原文

- [V1 Architecture](./v1-architecture.md)
- [Network Design](./network-design.md)
- [Tunnel Protocol](./tunnel-protocol.md)
- [Election Design](./election-design.md)
- [Security](./security.md)
- [Codex Guide](./codex-guide.md)

## 当前 V1 方向

ACBH V1 使用低成本公网 IPv4 节点作为 Coordinator、artifact 同步节点、会话协调节点和 relay fallback 节点。

公网节点不直接运行 Minecraft 主服。真正运行 Minecraft Server、Fabric、Paper 或 Velocity 的，是本地 Host。

当前 Host 侧 relay 已具备：

```text
Public Node relay
  <-> Host Agent relay client
  <-> Host local Minecraft server or Velocity
```

PR29 会补齐 Player 侧本地 TCP proxy，形成 relay-only 端到端链路。
