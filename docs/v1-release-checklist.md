# V1 Release Smoke Checklist

Run this checklist before tagging a v1 release or PR merge.

## 1. Coordinator

```bash
pnpm build:coordinator
pnpm --filter @acbh/coordinator test
```

Expected: TypeScript build passes, 98 tests pass.

Verify:

- [ ] `pnpm build:coordinator` succeeds
- [ ] All Coordinator tests pass

## 2. Agent Go tests and vet

```bash
cd agent
go test ./... -count=1
go vet ./...
```

Expected: All packages pass, vet produces no output.

Verify:

- [ ] `go test ./...` passes all packages (14 total)
- [ ] `go vet ./...` produces no warnings

## 3. Relay E2E smoke test

```bash
cd agent
go test ./internal/relay -run E2E -count=1 -v
```

Expected: `TestRelayE2ESmoke`, `TestRelayE2ENoSecretsInErrors`, `TestRelayE2ECancellationNoHang` all pass.

Verify:

- [ ] E2E single write echo works
- [ ] E2E multi-frame ordering works
- [ ] No secrets leak in error messages
- [ ] Context cancellation does not hang

## 4. Relay-only local demo

```bash
cd agent
go run ./cmd/relay-demo
```

Expected: `PASS` printed.

Verify:

- [ ] Demo outputs `PASS` without errors
- [ ] `examples/relay-only-demo/run.sh` works

## 5. Agent release packaging

```bash
./scripts/build-agent-release.sh
```

Expected: 10 binaries (5 platforms x 2 commands) and `checksums.txt` in `dist/`.

Verify:

- [ ] `dist/acbh-agent-linux-amd64 --help` works
- [ ] `dist/relay-demo-linux-amd64` passes
- [ ] `dist/checksums.txt` exists with all binaries listed
- [ ] `sha256sum -c dist/checksums.txt` succeeds (Linux)
- [ ] All binaries are statically linked (`file dist/*` shows no dynamic link)

## 6. GitHub Actions artifact workflow

Verify the workflow file is present and correct:

```bash
cat .github/workflows/agent-release-artifacts.yml
```

Expected: Uses `workflow_dispatch` and `v*` tag triggers, runs build script, uploads `dist/`.

Verify:

- [ ] Workflow file exists at `.github/workflows/agent-release-artifacts.yml`
- [ ] Uses existing `scripts/build-agent-release.sh` (no build logic duplication)
- [ ] No secrets or tokens required beyond `GITHUB_TOKEN`
- [ ] Artifact name is `acbh-agent-release-artifacts`

## 7. No generated artifacts tracked

```bash
git status --short
```

Expected: No files under `dist/` appear in git status.

Verify:

- [ ] `dist/` is in `.gitignore`
- [ ] No binaries or archives tracked by git

## 8. No secrets in logs or errors

Run and inspect output for token/sensitive strings:

```bash
cd agent
go test ./internal/relay -run Secrets -v
go test ./internal/playerproxy -run Token -v
go test ./internal/cli -run Secrets -v
```

Expected: All "NoSecretsInErrors" tests pass.

Verify:

- [ ] Host token never appears in error messages
- [ ] Player token never appears in error messages
- [ ] Raw binary payload bytes never appear in logs

## 9. Basic user-facing install and run flow

Simulate from a clean checkout:

```bash
# Build
./scripts/build-agent-release.sh

# Verify built binary
./dist/acbh-agent-linux-amd64 --help
./dist/acbh-agent-linux-amd64 doctor
./dist/acbh-agent-linux-amd64 relay host --help
./dist/acbh-agent-linux-amd64 relay player --help

# Run demo
./dist/relay-demo-linux-amd64
```

Expected: All commands produce help or meaningful output.

Verify:

- [ ] `acbh-agent --help` shows all subcommands
- [ ] `acbh-agent relay host --help` shows host relay options
- [ ] `acbh-agent relay player --help` shows player proxy options
- [ ] `relay-demo` runs and passes

## 10. Known limitations

The following are intentional V1 limitations:

- [ ] No P2P / direct transport (relies on Public Node relay)
- [ ] No GUI (CLI only)
- [ ] No auto-update mechanism
- [ ] No published GitHub Release (workflow artifacts only)
- [ ] No Minecraft protocol parsing
- [ ] No direct connection probing or STUN/ICE signaling
- [ ] No PostgreSQL persistence (Coordinator uses in-memory + JSON file)
- [ ] No WebRTC or QUIC transport

## Summary

- [ ] Coordinator build + tests
- [ ] Agent Go tests + vet
- [ ] Relay E2E smoke tests
- [ ] Relay demo runs
- [ ] Release packaging builds all binaries
- [ ] CI workflow file present
- [ ] No `dist/` artifacts tracked
- [ ] No secrets in logs
- [ ] User-facing commands work
- [ ] Known limitations documented
