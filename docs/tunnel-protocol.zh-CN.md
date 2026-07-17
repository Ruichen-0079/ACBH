[English](./tunnel-protocol.md) | [简体中文](./tunnel-protocol.zh-CN.md)

# 隧道协议

ACBH tunnel protocol 定义 Host、Player 与 Public Node relay 之间的控制和数据转发方式。

## 基本概念

### Player session

Player session 表示一个玩家端会话。

创建时可以返回一次性 raw player token。之后查询会话不应暴露 raw token。

Coordinator 内部应保存 token hash/verifier，而不是 raw token。

### Tunnel session

Tunnel session 表示一次 Host 与 Player 之间的 relay 会话。

一个 tunnel session 通常包含：

- groupId；
- sessionId；
- playerId；
- currentHostId；
- currentHostGeneration；
- status；
- expiration；
- runtime relay state。

runtime relay state 是临时状态，不应持久化。

## Host relay endpoint

Host Agent 连接：

```text
/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/host
```

需要 headers：

```text
X-ACBH-Host-ID
X-ACBH-Host-Token
X-ACBH-Host-Generation
```

Coordinator 应检查：

- group 是否存在；
- tunnel session 是否存在；
- tunnel session 是否未过期；
- tunnel session 是否处于 pending/active；
- Host 是否是 currentHost；
- generation 是否匹配；
- Host token 是否有效。

Host relay client 连接成功后，会连接本地 target address。

例如 Host 本地运行 Velocity：

```bash
acbh-agent relay host \
  --coordinator-url http://public-node:8080 \
  --group-id demo \
  --host-id host-a \
  --host-token xxx \
  --host-generation 3 \
  --session-id tunnel-session-id \
  --target-address 127.0.0.1:25577
```

## Player relay endpoint

Player local proxy 连接：

```text
/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/player
```

需要 headers：

```text
X-ACBH-Player-ID
X-ACBH-Player-Token
```

Coordinator 应检查：

- group 是否存在；
- tunnel session 是否存在；
- playerId 是否匹配；
- player token 是否有效；
- tunnel session 是否未过期；
- tunnel session 是否处于 pending/active。

Player local proxy 监听本地地址，例如：

```bash
acbh-agent relay player \
  --coordinator-url http://public-node:8080 \
  --group-id demo \
  --player-id player-a \
  --player-token xxx \
  --session-id tunnel-session-id \
  --listen-address 127.0.0.1:25565
```

玩家 Minecraft 客户端连接：

```text
127.0.0.1:25565
```

如果想用 25577 作为玩家本地端口：

```bash
acbh-agent relay player \
  --listen-address 127.0.0.1:25577
```

玩家客户端连接：

```text
127.0.0.1:25577
```

## 数据帧

relay MVP 使用 WebSocket binary frames。

规则：

- TCP bytes -> WebSocket binary frames；
- WebSocket binary frames -> TCP bytes；
- 不解析 Minecraft 协议；
- 不修改 payload；
- 不记录 raw payload；
- 任意一侧断开时关闭另一侧；
- context cancellation 必须主动关闭底层连接，避免阻塞读卡死。

## 会话状态

常见状态：

- pending：等待 Host 和 Player 接入；
- active：两端已配对并开始 relay；
- closed：正常关闭；
- failed：异常失败或超时。

当 Host 和 Player 都连接后，tunnel session 可以进入 active。

当任意一侧断开，relay pair 应清理，session 可进入 closed 或 failed。

## 手动 relay-only 流程

1. 创建 player session；
2. 创建 tunnel session；
3. 启动 Host relay；
4. 启动 Player local proxy；
5. 玩家 Minecraft 客户端连接本地 proxy。

Velocity 示例：

Host：

```bash
acbh-agent relay host \
  --coordinator-url http://public-node:8080 \
  --group-id demo \
  --host-id host-a \
  --host-token xxx \
  --host-generation 3 \
  --session-id tunnel-session-id \
  --target-address 127.0.0.1:25577
```

Player：

```bash
acbh-agent relay player \
  --coordinator-url http://public-node:8080 \
  --group-id demo \
  --player-id player-a \
  --player-token xxx \
  --session-id tunnel-session-id \
  --listen-address 127.0.0.1:25565
```

玩家客户端连接：

```text
127.0.0.1:25565
```
