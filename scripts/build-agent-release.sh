#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENT_DIR="$REPO_ROOT/agent"
DIST_DIR="$REPO_ROOT/dist"

VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo "dev")}"
GOOS="${GOOS:-}"

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "darwin/amd64"
  "darwin/arm64"
)

echo "=== ACBH Agent Release Build ==="
echo "Version: $VERSION"
echo "Output:  $DIST_DIR"
echo ""

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

for platform in "${PLATFORMS[@]}"; do
  goos="${platform%/*}"
  goarch="${platform#*/}"

  if [ -n "$GOOS" ] && [ "$GOOS" != "$goos" ]; then
    continue
  fi

  echo "Building $platform..."

  exe=""
  if [ "$goos" = "windows" ]; then
    exe=".exe"
  fi

  (
    cd "$AGENT_DIR"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
      -o "$DIST_DIR/acbh-agent-${goos}-${goarch}${exe}" \
      .
  )

  (
    cd "$AGENT_DIR"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
      -o "$DIST_DIR/relay-demo-${goos}-${goarch}${exe}" \
      ./cmd/relay-demo
  )

  echo "  acbh-agent-${goos}-${goarch}${exe}"
  echo "  relay-demo-${goos}-${goarch}${exe}"
done

echo ""
echo "=== Generating checksums ==="
(
  cd "$DIST_DIR"
  sha256sum -- * > checksums.txt 2>/dev/null || shasum -a 256 -- * > checksums.txt
)
echo "  $DIST_DIR/checksums.txt"

echo ""
echo "=== Build complete ==="
echo "Artifacts:"
ls -lh "$DIST_DIR"
