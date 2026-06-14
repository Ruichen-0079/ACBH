# v0.1-demo Draft Release 发布前核对

本文给维护者使用。当前 Draft Pre-release 已创建；本文不授权也不执行 Publish。

Draft URL：

<https://github.com/Ruichen-0079/ACBH/releases/tag/untagged-59d3def80f732b3256a1>

Draft URL 中出现 `untagged-*` 不一定表示绑定错误。发布前必须用 GitHub CLI 查询真实
字段：

```bash
gh release view v0.1-demo \
  --json tagName,name,isDraft,isPrerelease,url,assets
```

## 发布前检查

- [ ] `tagName` 是 `v0.1-demo`。
- [ ] tag 指向已审核的 main commit。
- [ ] `name` 是 `ACBH v0.1-demo`。
- [ ] `isDraft` 是 `true`。
- [ ] `isPrerelease` 是 `true`。
- [ ] release notes 已链接
      [单公网服务器双栈部署指南](deploy-single-vps-dual-stack.md)。
- [ ] `verify-all`、`demo-smoke`、`build-v0.1-demo` 全部 PASS。
- [ ] release assets 完整：
  - `acbh-agent-linux-amd64`
  - `acbh-agent-linux-arm64`
  - `acbh-agent-windows-amd64.exe`
  - `acbh-v0.1-demo-bundle.tar.gz`
  - `acbh-v0.1-demo-bundle.zip`
  - `SHA256SUMS`
- [ ] 用下载后的 `SHA256SUMS` 校验所有上传文件。
- [ ] release notes 和示例中没有真实 access key、Host token、Local Control token、
      RCON password 或其他凭据。
- [ ] 仓库工作区干净，`dist/` 和二进制产物未提交。
- [ ] 文档 PR 已合并，tag 所指提交符合最终发布决定。

## SHA256 校验

Linux：

```bash
sha256sum -c SHA256SUMS
```

Windows PowerShell 可逐项使用：

```powershell
Get-FileHash -Algorithm SHA256 .\acbh-agent-windows-amd64.exe
```

## 点击 Publish 前的最后确认

在 GitHub 页面再次确认 Draft 与 Pre-release 标记、tag、标题、notes 和 assets。若
`tagName` 不是 `v0.1-demo`、tag commit 不正确、资产不完整或任何校验失败，停止发布。

正式 Publish 必须由维护者在文档 PR 合并后单独确认；不要因为 Draft URL 含
`untagged-*` 就重建或强推 tag。
