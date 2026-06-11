[English](./security.md) | [简体中文](./security.zh-CN.md)

# 安全说明

ACBH 涉及 Host token、Player token、takeover token、artifact、Minecraft payload 等敏感数据。实现和运维时必须避免泄露。

## Token 原则

不要记录：

- host token；
- player token；
- takeover token；
- raw Minecraft payload；
- WebSocket binary frame payload。

错误信息可以包含：

- group ID；
- session ID；
- host ID；
- player ID；
- HTTP status code；
- listen address；
- target address。

错误信息不应包含任何 token。

## Takeover token

takeover token 是一次性凭据。

Coordinator 不应持久化 raw takeover token。

可以保存：

- token hash；
- verifier；
- 过期时间；
- assignment state。

dry-run 不应生成、保存或消耗 takeover token。

## Host generation

Host generation 用来防止 stale Host 写入或继续服务。

涉及以下行为时必须检查 generation：

- artifact upload；
- latest pointer 更新；
- Host relay connection；
- takeover complete；
- current Host 状态变更。

过期 generation 的 Host 应被拒绝。

## Player local proxy

Player local proxy 默认应绑定 loopback：

```text
127.0.0.1
```

例如：

```bash
--listen-address 127.0.0.1:25565
```

如果用户显式设置：

```bash
--listen-address 0.0.0.0:25565
```

或：

```bash
--listen-address 0.0.0.0:25577
```

这会让局域网内其他设备也可能连接到该代理。文档必须明确提示风险。

## Relay payload

relay 层只转发 opaque bytes。

不要：

- 打印 Minecraft payload；
- 持久化 payload；
- 将 payload 写入测试快照；
- 在错误信息中包含原始 payload。

可以记录：

- 连接建立；
- 连接关闭；
- session ID；
- host ID；
- player ID；
- byte counters；
- close reason；
- 非敏感错误摘要。

## Runtime relay state

runtime relay state 是临时状态，不应持久化。

Coordinator 重启后，应由 Host/Player 重新建立 session 和 relay connection。

## Artifact 安全

上传 artifact 时应检查：

- artifact class；
- manifest schema；
- hash；
- 删除标记；
- zero-byte 文件；
- 路径安全；
- generation；
- current Host 权限。

路径必须防止：

- `..`；
- 绝对路径；
- Windows drive path；
- symlink escape。

## RCON

RCON password 不应写入日志。

safe-sync 应在扫描前触发保存流程，例如：

- save-all；
- flush；
- 等待保存完成。

RCON 失败时不应继续扫描并上传不一致状态。
