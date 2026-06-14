# Agent Release Packaging

## Overview

The `scripts/build-agent-release.sh` script builds distributable Agent binaries
for multiple platforms.

## v0.1-demo packaging

### Quick build

```bash
bash scripts/build-v0.1-demo.sh
```

### What is built

| Artifact | Description |
|---|---|
| `acbh-agent-linux-amd64` | Agent CLI for Linux x86_64 |
| `acbh-agent-windows-amd64.exe` | Agent CLI for Windows x86_64 |
| `acbh-agent-linux-arm64` | Agent CLI for Linux ARM64 (optional, skipped on failure) |
| `coordinator/` | Built Coordinator (TypeScript, Node.js) |
| `docs/` | Documentation: demo, release notes, checklist, security, packaging |
| `README.md` / `README.zh-CN.md` | Project READMEs |
| `.env.example` | Environment variable template |
| `agent/config.example.json` | Agent config template |
| `scripts/verify-all.sh` | Linux/macOS verification script |
| `scripts/verify-all.ps1` | Windows PowerShell verification script |
| `scripts/demo-smoke.sh` | CLI demo smoke script |
| `SHA256SUMS` | SHA256 checksums for all artifacts |

### Output directory

```
dist/release/v0.1-demo/
```

### Skipping arm64

```bash
ACBH_SKIP_ARM64=1 bash scripts/build-v0.1-demo.sh
```

### Verify checksums

```bash
cd dist/release/v0.1-demo
sha256sum -c SHA256SUMS       # Linux
shasum -a 256 -c SHA256SUMS    # macOS
```

On Windows (PowerShell):

```powershell
Get-FileHash -Algorithm SHA256 (Get-ChildItem -File -Exclude SHA256SUMS) | ForEach-Object {
  $expected = (Select-String -Path SHA256SUMS -Pattern $_.Path).Line.Split(" ")[0]
  if ($_.Hash -ne $expected) { Write-Error "Mismatch: $($_.Path)" }
}
```

### Fedora manual upload steps

1. Build locally: `bash scripts/build-v0.1-demo.sh`
2. Verify checksums: `cd dist/release/v0.1-demo && sha256sum -c SHA256SUMS`
3. Upload the directory contents to a GitHub Release or shared drive.
4. On a target Fedora machine:
   ```bash
   tar xzf acbh-v0.1-demo.tar.gz
   cd v0.1-demo
   sha256sum -c SHA256SUMS
   chmod +x acbh-agent-linux-amd64
   ```
5. Start the Coordinator: `cd coordinator && node index.js`

### Windows manual upload steps

1. Build on Linux (cross-compile) or on Windows (native Go build).
2. If using the cross-compiled `acbh-agent-windows-amd64.exe`, no extra
   dependencies are needed — the binary is statically linked.
3. Copy `dist/release/v0.1-demo/` to the Windows machine.
4. Verify in PowerShell:
   ```powershell
   Set-Location dist\release\v0.1-demo
   Get-FileHash -Algorithm SHA256 acbh-agent-windows-amd64.exe
   ```
   Compare with the hash in SHA256SUMS.
5. Run the Agent: `.\acbh-agent-windows-amd64.exe --help`

### GitHub Release manual upload

After building:

1. Go to the repository on GitHub.
2. Create a new Release (do not use this script to auto-publish).
3. Upload each file from `dist/release/v0.1-demo/` as a release asset,
   or create a `.zip` / `.tar.gz` archive and upload that.
4. Copy the SHA256SUMS content into the release notes.

### Important

- Generated binaries in `dist/` and `dist/release/` are ignored by
  `.gitignore`. Do NOT commit them.
- Example config files use placeholders (`change-me`, `grp_example`, etc.).
  Never commit real credentials.
- The Coordinator `dist/` output requires Node.js 20+ at runtime.

## Required tools

- Go 1.22+
- Bash
- `sha256sum` or `shasum` (included with most OSes)

## How to build

From the repo root:

```bash
./scripts/build-agent-release.sh
```

To build for a single platform:

```bash
GOOS=linux ./scripts/build-agent-release.sh
```

To include a version label (defaults to `git describe` or `dev`):

```bash
VERSION=v1.0.0 ./scripts/build-agent-release.sh
```

## What is built

| Binary | Source | Description |
|---|---|---|
| `acbh-agent-{os}-{arch}` | `agent/` (package root) | Main ACBH Agent CLI |
| `relay-demo-{os}-{arch}` | `agent/cmd/relay-demo` | Relay-only demo runner |

## Platforms

| GOOS | GOARCH | Extension |
|---|---|---|
| linux | amd64 | _(none)_ |
| linux | arm64 | _(none)_ |
| windows | amd64 | `.exe` |
| darwin | amd64 | _(none)_ |
| darwin | arm64 | _(none)_ |

All binaries are built with `CGO_ENABLED=0` for static linking.

## Where artifacts are written

Artifacts are written to `dist/` in the repo root:

```
dist/
  acbh-agent-linux-amd64
  acbh-agent-linux-arm64
  acbh-agent-windows-amd64.exe
  acbh-agent-darwin-amd64
  acbh-agent-darwin-arm64
  relay-demo-linux-amd64
  relay-demo-linux-arm64
  relay-demo-windows-amd64.exe
  relay-demo-darwin-amd64
  relay-demo-darwin-arm64
  checksums.txt
```

## Running the built acbh-agent

```bash
./dist/acbh-agent-linux-amd64 relay host --help
./dist/acbh-agent-linux-amd64 relay player --help
```

On macOS:

```bash
./dist/acbh-agent-darwin-arm64 relay host --help
```

On Windows:

```
dist\acbh-agent-windows-amd64.exe relay host --help
```

## Running the relay-only demo from built artifacts

The demo uses an in-memory relay server and does not need a Coordinator.

```bash
./dist/relay-demo-linux-amd64
```

With custom addresses:

```bash
./dist/relay-demo-linux-amd64 \
  --host-target 127.0.0.1:25577 \
  --player-listen 127.0.0.1:25565
```

On Windows:

```
dist\relay-demo-windows-amd64.exe
```

## Checksums

`dist/checksums.txt` contains SHA256 hashes for all built artifacts.

Verify on Linux:

```bash
cd dist && sha256sum -c checksums.txt
```

Verify on macOS:

```bash
cd dist && shasum -a 256 -c checksums.txt
```

## Platform notes

### Windows (.exe)

Windows binaries have the `.exe` extension. Use the same commands with `.exe`
appended to the binary name.

### macOS (darwin)

Apple Silicon (M1/M2/M3) Macs should use `darwin-arm64`.
Intel Macs should use `darwin-amd64`.

## Git

Generated artifacts (`dist/`) are ignored by `.gitignore` and should never be
committed to the repository.

## CI workflow

A GitHub Actions workflow is available at
`.github/workflows/agent-release-artifacts.yml`.

### How to run manually

1. Go to the repository on GitHub.
2. Click the **Actions** tab.
3. Select **Agent Release Artifacts** from the left sidebar.
4. Click the **Run workflow** dropdown.
5. Select the branch and click **Run workflow**.

The workflow will also run automatically when a tag matching `v*` is pushed
(e.g. `v1.0.0`).

### Where to download artifacts

After the workflow completes, the generated artifacts are available as a
workflow artifact at the bottom of the workflow run page, named
`acbh-agent-release-artifacts`.

The artifact is a ZIP file containing:
- `acbh-agent-{os}-{arch}` binaries
- `relay-demo-{os}-{arch}` binaries
- `checksums.txt`

### Important notes

- This workflow does **not** publish a GitHub Release. It only produces
  workflow artifacts that are available for download from the Actions tab.
- Workflow artifacts are **temporary**. GitHub automatically expires them
  after 90 days by default.
- To create a permanent GitHub Release, download the workflow artifact and
  attach the binaries to a release page.
- The workflow does not require any secrets or tokens beyond the default
  `GITHUB_TOKEN`.

