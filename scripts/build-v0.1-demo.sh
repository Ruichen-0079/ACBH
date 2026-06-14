#!/usr/bin/env bash
# Build v0.1-demo release artifacts
# Usage: bash scripts/build-v0.1-demo.sh
# Output: dist/release/v0.1-demo/
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENT_DIR="$REPO_ROOT/agent"
COORD_DIR="$REPO_ROOT/apps/coordinator"
STAGING="$REPO_ROOT/dist/release/v0.1-demo"

echo "=== ACBH v0.1-demo Packaging ==="
echo "Staging: $STAGING"
echo ""

# ── Clean ─────────────────────────────────────────
rm -rf "$STAGING"
mkdir -p "$STAGING"

# ── Build Coordinator ─────────────────────────────
echo "--- Building Coordinator ---"
(cd "$REPO_ROOT" && pnpm build:coordinator)

# ── Build Agent binaries ──────────────────────────
echo "--- Building Agent (linux/amd64) ---"
(
  cd "$AGENT_DIR"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o "$STAGING/acbh-agent-linux-amd64" \
    .
)
echo "  acbh-agent-linux-amd64"

echo "--- Building Agent (windows/amd64) ---"
(
  cd "$AGENT_DIR"
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o "$STAGING/acbh-agent-windows-amd64.exe" \
    .
)
echo "  acbh-agent-windows-amd64.exe"

# Optional: linux/arm64
if [ "${ACBH_SKIP_ARM64:-}" != "1" ]; then
  echo "--- Building Agent (linux/arm64) ---"
  (
    cd "$AGENT_DIR"
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
      go build -trimpath -ldflags="-s -w" \
      -o "$STAGING/acbh-agent-linux-arm64" \
      .
  ) && echo "  acbh-agent-linux-arm64" || echo "  (skipped — arm64 build failed, continuing)"
fi

# ── Copy Coordinator dist ─────────────────────────
echo "--- Copying Coordinator ---"
mkdir -p "$STAGING/coordinator"
cp -r "$COORD_DIR/dist"/* "$STAGING/coordinator/" 2>/dev/null || true
cp "$COORD_DIR/package.json" "$STAGING/coordinator/"

# ── Copy docs & configs ───────────────────────────
echo "--- Copying docs ---"
for f in \
  README.md \
  README.zh-CN.md \
  docs/demo.md \
  docs/release-notes-v0.1-demo.md \
  docs/release-checklist.md \
  docs/security.md \
  docs/release-packaging.md \
  .env.example \
  agent/config.example.json \
; do
  mkdir -p "$STAGING/$(dirname "$f")"
  cp "$REPO_ROOT/$f" "$STAGING/$f"
  echo "  $f"
done

# ── Copy scripts ──────────────────────────────────
echo "--- Copying scripts ---"
for f in \
  scripts/verify-all.sh \
  scripts/verify-all.ps1 \
  scripts/demo-smoke.sh \
; do
  mkdir -p "$STAGING/$(dirname "$f")"
  cp "$REPO_ROOT/$f" "$STAGING/$f"
  chmod +x "$STAGING/$f"
  echo "  $f"
done

# ── Generate SHA256SUMS ───────────────────────────
echo "--- Generating SHA256SUMS ---"
(
  cd "$STAGING"
  find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)
echo "  SHA256SUMS"

# ── Summary ───────────────────────────────────────
echo ""
echo "=== Packaging complete ==="
echo "Staging: $STAGING"
echo ""
echo "Artifacts:"
find "$STAGING" -type f ! -name SHA256SUMS -print | sort | while read -r f; do
  echo "  ${f#$STAGING/}"
done
echo ""
echo "Checksums: $STAGING/SHA256SUMS"
