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

  if [ "$goos" = "windows" ]; then
    (
      cd "$AGENT_DIR"
      CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
        -o "$DIST_DIR/acbh-desktop-${goos}-${goarch}${exe}" \
        ./cmd/acbh-desktop
    )
  fi

  (
    cd "$AGENT_DIR"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
      -o "$DIST_DIR/relay-demo-${goos}-${goarch}${exe}" \
      ./cmd/relay-demo
  )

  echo "  acbh-agent-${goos}-${goarch}${exe}"
  if [ "$goos" = "windows" ]; then
    echo "  acbh-desktop-${goos}-${goarch}${exe}"
  fi
  echo "  relay-demo-${goos}-${goarch}${exe}"
done

if [ -f "$DIST_DIR/acbh-desktop-windows-amd64.exe" ]; then
  echo ""
  echo "=== Building private desktop bundle ==="
  BUNDLE_NAME="acbh-v0.3-private-desktop-bundle"
  BUNDLE_PARENT="$(mktemp -d)"
  BUNDLE_ROOT="$BUNDLE_PARENT/$BUNDLE_NAME"
  rm -rf "$DIST_DIR/$BUNDLE_NAME" "$DIST_DIR/$BUNDLE_NAME.zip"
  mkdir -p "$BUNDLE_ROOT/scripts"
  mkdir -p "$BUNDLE_ROOT/docs"
  cp "$DIST_DIR/acbh-desktop-windows-amd64.exe" "$BUNDLE_ROOT/"
  cp "$DIST_DIR/acbh-agent-windows-amd64.exe" "$BUNDLE_ROOT/"
  cp "$REPO_ROOT/scripts/acbh-desktop-gui.ps1" "$BUNDLE_ROOT/scripts/"
  cp -R "$REPO_ROOT/docs/zh-CN" "$BUNDLE_ROOT/docs/"
  mkdir -p "$BUNDLE_ROOT/coordinator/dist"
  cp "$REPO_ROOT/apps/coordinator/package.json" "$BUNDLE_ROOT/coordinator/"
  cp -R "$REPO_ROOT/apps/coordinator/dist/." "$BUNDLE_ROOT/coordinator/dist/"
  if command -v npm >/dev/null 2>&1; then
    (cd "$BUNDLE_ROOT/coordinator" && npm install --omit=dev --no-audit --no-fund --package-lock=false)
  else
    echo "npm not found; cannot build self-contained coordinator dependencies" >&2
    exit 1
  fi
  cp -R "$BUNDLE_ROOT" "$DIST_DIR/"
  if command -v zip >/dev/null 2>&1; then
    (cd "$BUNDLE_ROOT" && zip -qr "$DIST_DIR/$BUNDLE_NAME.zip" .)
  elif command -v powershell.exe >/dev/null 2>&1; then
        BUNDLE_ROOT_WIN="$(cygpath -w "$BUNDLE_ROOT")"
    BUNDLE_ZIP_WIN="$(cygpath -w "$DIST_DIR/$BUNDLE_NAME.zip")"
    powershell.exe -NoProfile -Command "Compress-Archive -Path '$BUNDLE_ROOT_WIN\\*' -DestinationPath '$BUNDLE_ZIP_WIN' -Force"
  else
    echo "zip not found and PowerShell Compress-Archive unavailable" >&2
    exit 1
  fi
  rm -rf "$BUNDLE_PARENT"
  echo "  $DIST_DIR/$BUNDLE_NAME.zip"
fi

echo ""
echo "=== Generating checksums ==="
(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    find . -maxdepth 1 -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
  else
    find . -maxdepth 1 -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 shasum -a 256 > SHA256SUMS
  fi
)
echo "  $DIST_DIR/SHA256SUMS"

echo ""
echo "=== Build complete ==="
echo "Artifacts:"
ls -lh "$DIST_DIR"

