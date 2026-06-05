# Codex Implementation Guide

Read this before implementing ACBH tasks.

## Project rule

Do not implement hot migration.

ACBH V1 is file-level snapshot sync plus fast host takeover.

## First implementation target

Create a runnable repository skeleton:

- Coordinator starts and returns `/health`.
- Agent CLI starts and returns help.
- `doctor` command prints basic local checks.
- CI runs TypeScript build and Go tests.

## Preferred order

1. Keep docs accurate.
2. Bootstrap Coordinator.
3. Bootstrap Agent CLI.
4. Add local storage interface.
5. Add manifest generation.
6. Add RCON safe sync.
7. Add heartbeat and election.
8. Build Host A to Host B demo.

## Constraints

- Do not introduce a proxy layer in V1.
- Do not require Minecraft mods.
- Do not parse chunk data.
- Do not upload files directly from fsnotify events.
- Do not store secrets in plaintext.
- Do not let stale hosts overwrite the latest snapshot.
- Do not mix server-pack changes with world snapshots.
- Do not auto-accept local Host changes to mods, plugins, config, or launch metadata.
- Do not treat all server files as one snapshot.

## Useful issues

- #1 V1 MVP flow and non-goals
- #2 Snapshot manifest and server pack format
- #3 Coordinator backend
- #5 Agent CLI
- #7 Safe snapshot sync
- #9 Host election
- #11 End-to-end takeover demo
