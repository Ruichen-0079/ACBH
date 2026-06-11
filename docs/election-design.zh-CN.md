[English](./election-design.md) | [简体中文](./election-design.zh-CN.md)

# 选举设计

ACBH 的选举系统用于在当前 Host 不可用时选择新的 Host，并触发接管流程。

## 目标

选举系统需要：

- 判断当前 Host 是否失效；
- 找出可用候选 Host；
- 计算 Host Score；
- 选择最佳 Host；
- 发起 takeover assignment；
- 避免 stale Host 继续写入；
- 只在 takeover 完成后更新 currentHostGeneration。

## Host 心跳

Host 通过 heartbeat 向 Coordinator 报告状态。

常见状态包括：

- standby；
- hosting；
- offline；
- unknown。

心跳中可以包含：

- host ID；
- group ID；
- status；
- observed network info；
- election hints；
- capability hints；
- local server hints。

## 候选资格

候选 Host 应满足：

- 最近有有效 heartbeat；
- Host token 有效；
- 未被禁用；
- 支持所需能力；
- 可以恢复所需 artifact；
- 不处于明显不健康状态。

## Host Score

Host Score 应尽量确定性。

可以考虑：

- 最近心跳时间；
- 是否在线；
- 是否已拥有最新 artifact；
- 机器能力；
- 网络可达性；
- 历史稳定性；
- 用户优先级。

同分时应使用确定性 tie-breaker，例如 host ID 字典序。

## Takeover assignment

选举出新 Host 后，Coordinator 创建 takeover assignment。

assignment 应包含：

- assignment ID；
- group ID；
- target host ID；
- expected generation；
- restore artifact pointers；
- one-time takeover token；
- token hash/verifier；
- expiration；
- status。

raw takeover token 只能返回一次，不应持久化。

## 接管流程

推荐流程：

1. Host poll assignment；
2. Host accept assignment；
3. Host 按顺序恢复：
   - server-pack；
   - admin-state；
   - world-snapshot；
4. Host 启动服务端；
5. Host 发送 hosting heartbeat；
6. Host complete takeover；
7. Coordinator 更新 currentHostId；
8. Coordinator 递增 currentHostGeneration。

只有 takeover complete 后才递增 generation。

## 失败处理

如果接管失败：

- Host 应报告 fail；
- Coordinator 记录失败原因；
- assignment 进入 failed；
- 可以重新选举或等待人工干预。

失败原因不应包含 secret。

## Dry run

dry-run 选举或接管检查不应：

- 生成 raw takeover token；
- 消耗 token；
- 修改 currentHost；
- 修改 generation；
- 写入持久状态。

dry-run 只用于验证候选、状态和恢复计划。
