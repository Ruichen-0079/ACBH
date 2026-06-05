# Dependency Plan

This document records the V1 library choices so future implementation agents do not over-engineer the first milestone.

## Agent

Language: Go.

Recommended dependencies:

- CLI: `github.com/spf13/cobra`
- Config: `github.com/spf13/viper`
- File events: `github.com/fsnotify/fsnotify`
- RCON: evaluate `github.com/gorcon/rcon` or a minimal custom Minecraft RCON client

Rules:

- File events are only dirty-file hints.
- Do not upload files immediately after a filesystem event.
- Safe sync must use RCON `save-all flush` before manifest generation.

## Coordinator

Language: TypeScript.

Recommended dependencies:

- HTTP server: Fastify first
- Config: dotenv-compatible environment variables
- Database: PostgreSQL later, in-memory/local mock acceptable for skeleton
- ORM: Drizzle or Prisma after schema is locked

## Storage

V1:

- Local filesystem storage
- Content-addressed objects keyed by SHA256
- Snapshot manifests stored as JSON

V1.5:

- S3-compatible storage via MinIO SDK or AWS-compatible APIs
- Candidate providers: MinIO, Cloudflare R2, AWS S3, Backblaze B2 S3

## Network

V1:

- External Tailscale or Headscale setup
- ACBH stores current host connection metadata only

Not in V1:

- Custom NAT traversal
- Custom relay
- Transparent proxy

## Backup reference

Restic is useful as a design reference for snapshot/backup semantics, but V1 should not embed it. ACBH needs explicit Coordinator-managed snapshot validity, host election, and latest snapshot state.
