# Agent Release Packaging

## Overview

The `scripts/build-agent-release.sh` script builds distributable Agent binaries
for multiple platforms.

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

