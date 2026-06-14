# ACBH Release Checklist

Run before tagging a release. All items must pass.

## 1. Repository state

- [ ] Working on a clean branch (no uncommitted changes)
- [ ] All intended PRs merged to the release branch
- [ ] `git status` shows no unexpected files

## 2. Build and tests

```bash
# Go
cd agent && go vet ./... && go test ./... -count=1

# Coordinator
pnpm build:coordinator && pnpm --filter @acbh/coordinator test
```

Or equivalently:

```bash
bash scripts/verify-all.sh       # Linux / Fedora / macOS
powershell scripts/verify-all.ps1 # Windows
```

- [ ] `go vet` clean
- [ ] `go test ./... -count=1` all pass
- [ ] `pnpm build:coordinator` succeeds
- [ ] `pnpm --filter @acbh/coordinator test` all pass (minimum 123 tests)
- [ ] No `FAIL` lines in test output

## 3. Demo smoke

```bash
bash scripts/demo-smoke.sh
```

- [ ] `ACBH Demo Smoke: ALL CHECKS PASSED`
- [ ] Covers: build, health, group create, register host, heartbeat, scan, push, latest, pull, group state
- [ ] Demo uses temporary config/storage paths and removes them on exit
- [ ] Demo stops Coordinator on success, error, and interrupt
- [ ] Demo curl authentication and JSON bodies are passed through protected files, not literal argv

## 4. Security checklist

- [ ] Local Control API defaults to `127.0.0.1:6122` (loopback only)
- [ ] `--allow-remote-control` prints a warning when used
- [ ] Dashboard does not persist `accessKey`, `hostToken`, `agentToken`, `rconPassword` to localStorage
- [ ] Dashboard `saveLocal()` only saves non-secret keys
- [ ] Dashboard `forgetSecrets()` clears both localStorage and form fields
- [ ] Dashboard does not use `sessionStorage`, IndexedDB, URL query strings, fragments, or console logs for secrets
- [ ] Dashboard clears Local Control token after 401/403
- [ ] Dashboard warns and confirms before connecting to a non-loopback Local Control URL
- [ ] Dashboard confirms stop, restore, and takeover operations
- [ ] Dashboard command generation does not interpolate access keys or RCON passwords into argv
- [ ] Control token stored with `0600` permissions
- [ ] Control token masked in logs/stdout output
- [ ] Auth errors never contain plaintext token values
- [ ] Group state response does not contain `accessKey` or `hostToken`
- [ ] Manifest validation rejects path escape (`../`)
- [ ] GC fails closed when retained manifest read fails
- [ ] Player token rejected when `expiresAt <= now`
- [ ] No hardcoded secrets in source, scripts, docs, or fixtures
- [ ] `.env.example` and `config.example.json` use `change-me` placeholders only
- [ ] Recommended docs and demo commands do not put credentials in process arguments

## 5. Manifest schema

- [ ] Shared fixtures pass in both Go (`agent/internal/manifest/testdata/`) and TS (`apps/coordinator/test/fixtures/`)
- [ ] All valid fixtures accepted by both sides
- [ ] All invalid fixtures rejected by both sides
- [ ] `manifestVersion` optional semantics consistent
- [ ] `class` field required; `fileClass` normalized; `ignored`/`unknown` rejected
- [ ] Summary consistency enforced
- [ ] Class-artifact compatibility enforced

## 6. Platform checks

- [ ] Go tests pass on Linux (Fedora)
- [ ] Go tests pass on Windows (PowerShell)
- [ ] Coordinator builds and tests pass on both platforms
- [ ] Windows path handling: backslash normalization, drive-letter paths, reserved device names
- [ ] Shell demo script works on Linux (`bash scripts/demo-smoke.sh`)
- [ ] PowerShell verify script works on Windows (`powershell scripts/verify-all.ps1`)

## 7. Docs

- [ ] `README.md` quick start instructions work end-to-end
- [ ] `docs/security.md` reflects current security posture
- [ ] `docs/demo.md` or equivalent explains how to run the demo
- [ ] GUI demo covers group, host, Local Control, server status, artifacts, election, and events
- [ ] `docs/release-checklist.md` (this file) is up to date
- [ ] No broken links between docs

## 8. Config / Env

- [ ] `.env.example` documents all env vars
- [ ] `agent/config.example.json` shows full config schema
- [ ] No real credentials in example files

## 9. Notes for release

- [ ] `CHANGELOG.md` or equivalent updated (if exists)
- [ ] Release notes summarize security defaults, demo scope, and known limitations ([`release-notes-v0.1-demo.md`](release-notes-v0.1-demo.md))
- [ ] Version string consistent (agent `agentconfig.AgentVersion`, coordinator `package.json`)
- [ ] Release artifacts build (`bash scripts/build-agent-release.sh`)
