# ACBH v0.4.0-alpha2 Hotfix

## 修复

### Windows GUI 长操作阻塞 / 未响应

- 引入统一后台任务执行器（`BackgroundWorker` + UI `BeginInvoke`），所有 Agent 调用、状态刷新、世界备份、启停服等长操作离开 UI 线程。
- 点击后 100ms 内显示“处理中”、Marquee 进度条，并禁用冲突按钮；窗口可继续拖动、重绘和查看日志。
- 防止重复点击与互斥操作并发（启动/停止/备份/恢复）。
- `exit code=0` 但 JSON `ok=false` 时显示失败，不再记录“完成”。
- StopAuto 世界备份失败时返回 `partialFailure` 结构化状态。

### 世界对象上传 Reader 所有权

- `UploadWorldObjectStream` 使用 `io.NopCloser` 包装请求体，HTTP 传输不再关闭调用方 `*os.File`。
- 抽取 `worldbackup.UploadMissingObjects` 供 CLI、Desktop stopped、online/resume 共用。

### Minecraft 启动 readiness

- 新增 `server.startTimeout` 配置（默认 `120s`，最小 `10s`）。
- 等待期间每 750ms 检查本地端口；进程提前退出立即失败并附日志摘要。
- 超时时错误包含端口、等待时长、服务端命令、日志路径。

### Coordinator Linux Bundle Shell LF

- 新增 `.gitattributes`：`*.sh text eol=lf`。
- `build-agent-release.sh` 打包前检测 BOM/CRLF 并规范化为 LF。

### VPS health 等待

- 新增 `acbh_vps_wait_for_health`（默认 60s、每秒重试、version 匹配）。
- 升级与自动回滚在 `systemctl start` 后使用同一等待机制。

## 配置

```json
{
  "server": {
    "startTimeout": "120s"
  }
}
```

## 已知限制

- Agent 尚未输出 NDJSON 进度事件；GUI 使用阶段日志 + 不确定进度条。
- Manifest commit 等事务阶段不可安全取消。