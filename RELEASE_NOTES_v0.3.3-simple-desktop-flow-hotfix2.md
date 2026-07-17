# ACBH v0.3.3-simple-desktop-flow-hotfix2

本 hotfix 修复 Windows GUI 环境检查阶段读取 Agent JSON stdout 时的编码问题。

## 修复

- GUI 启动 `acbh-agent` 子进程时显式设置 `StandardOutputEncoding` / `StandardErrorEncoding` 为 UTF-8。
- 避免 PowerShell/.NET 按本机代码页解码 Agent 输出，导致中文字段乱码并使 `ConvertFrom-Json` 报错。
- Windows bundle smoke test 增加环境检查 JSON 编码回归：
  - 执行 `desktop environment check --json`。
  - 确认 stdout 可直接 `ConvertFrom-Json`。
  - 确认输出中不包含常见 mojibake/replacement 字符。

## 说明

- hotfix1 解决“脚本文件自身被 PowerShell 5.1 错误解码”。
- hotfix2 解决“GUI 读取 Agent JSON stdout 时错误解码”。
- 不改变 v0.3.3 的功能流程、relay、sync、current-host 逻辑。
