[English](./codex-guide.md) | [简体中文](./codex-guide.zh-CN.md)

# Codex 开发指南

本文档用于指导 AI 编程工具或贡献者在 ACBH 仓库中安全、可控地实现功能。

## 总原则

每个 PR 应保持小而聚焦。

不要在一个 PR 中混入：

- runtime 代码；
- 大规模重构；
- 文档翻译；
- 依赖升级；
- 测试框架调整；
- release packaging。

如果任务不同，应拆成多个 PR。

## 当前项目方向

ACBH V1 的公网 IPv4 节点不是 Minecraft 主服。

它是：

- Coordinator；
- artifact sync node；
- relay fallback node；
- rendezvous / control plane。

真正的 Minecraft server、Fabric、Paper 或 Velocity 运行在本地 Host。

## PR 边界

已经完成的方向：

- PR21：选举和接管；
- PR22：stale Host artifact 保护；
- PR23：daemon opt-in auto takeover；
- PR24：artifact GC；
- PR25：Coordinator 状态持久化；
- PR26：网络与 relay 架构基础；
- PR27：Public Relay MVP；
- PR28：Host Agent relay client。

PR29 目标：

- Player local TCP proxy；
- 本地监听可配置地址；
- 转发 Minecraft 客户端 TCP bytes 到 Public Node relay；
- 不实现 P2P；
- 不做 release packaging；
- 不解析 Minecraft 协议。

## 端口规则

不要硬编码 25565。

Host target address 和 Player listen address 是独立配置。

Velocity 示例：

```bash
acbh-agent relay host \
  --target-address 127.0.0.1:25577
```

Player 本地代理可以监听：

```bash
acbh-agent relay player \
  --listen-address 127.0.0.1:25565
```

也可以监听：

```bash
acbh-agent relay player \
  --listen-address 127.0.0.1:25577
```

文档和测试都应体现该可配置性。

## 安全规则

不要打印：

- host token；
- player token；
- takeover token；
- raw Minecraft payload；
- WebSocket binary frame payload。

测试中不要使用看起来像真实 secret 的值。

## 代码风格

Go 代码：

- 保持 package 边界清晰；
- relay/client/proxy 逻辑要可测试；
- context cancellation 必须可靠清理连接；
- TCP Read 阻塞时，仅取消 context 不够，必须关闭底层连接；
- 错误信息不要包含 secret。

TypeScript 代码：

- 保持 Coordinator routes 和 store 边界清晰；
- 不要把 raw token 放进持久状态；
- 测试要覆盖错误路径；
- 修改 API 时同步更新 docs。

## 验证命令

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

完整：

```bash
pnpm build:coordinator
pnpm --filter @acbh/coordinator test
cd agent && go test ./... -count=1
cd agent && go vet ./...
```

## PR 检查清单

提交前检查：

```bash
git status
git diff --name-status
git diff --check
```

确认：

- 文件范围符合 PR 目标；
- 没有临时文件；
- 没有真实 token；
- 没有 payload dump；
- lockfile 只在依赖变化时更新；
- 文档和测试同步更新。
