#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENT_DIR="$REPO_ROOT/agent"
DIST_DIR="$REPO_ROOT/dist"

VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo "dev")}"
GOOS="${GOOS:-}"

zip_dir() {
  local source_dir="$1"
  local target_zip="$2"
  if command -v zip >/dev/null 2>&1; then
    (cd "$source_dir" && zip -qr "$target_zip" .)
  elif command -v powershell.exe >/dev/null 2>&1; then
    local source_win target_win
    source_win="$(cygpath -w "$source_dir")"
    target_win="$(cygpath -w "$target_zip")"
    powershell.exe -NoProfile -Command "Compress-Archive -Path '$source_win\\*' -DestinationPath '$target_win' -Force"
  else
    echo "zip not found and PowerShell Compress-Archive unavailable" >&2
    exit 1
  fi
}

copy_private_node_runtime() {
  local target_dir="$1"
  local node_bin=""
  node_bin="$(command -v node.exe 2>/dev/null || true)"
  if [ -z "$node_bin" ]; then
    node_bin="$(command -v node 2>/dev/null || true)"
  fi
  if [ -z "$node_bin" ] || [ ! -f "$node_bin" ]; then
    echo "node executable not found; cannot assemble private desktop runtime" >&2
    exit 1
  fi
  mkdir -p "$target_dir/runtime/node"
  cp "$node_bin" "$target_dir/runtime/node/node.exe"
}

copy_powershell_utf8_bom() {
  local source_file="$1"
  local target_file="$2"
  cp "$source_file" "$target_file"
  if command -v powershell.exe >/dev/null 2>&1; then
    local target_win
    target_win="$(cygpath -w "$target_file")"
    powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "\$path='$target_win'; \$text=[System.IO.File]::ReadAllText(\$path, [System.Text.Encoding]::UTF8); \$utf8Bom=New-Object System.Text.UTF8Encoding \$true; [System.IO.File]::WriteAllText(\$path, \$text, \$utf8Bom)"
  else
    printf '\xEF\xBB\xBF' > "$target_file.bom"
    cat "$target_file" >> "$target_file.bom"
    mv "$target_file.bom" "$target_file"
  fi
}

build_runtime_package() {
  local package_root="$1"
  local target_zip="$2"
  local version="$3"
  local files_json=""
  local first=1
  for rel in "node/node.exe" "resources/runtime-base.README.txt"; do
    local file="$package_root/$rel"
    local hash size
    hash="$(sha256sum "$file" | awk '{print $1}')"
    size="$(wc -c < "$file" | tr -d ' ')"
    if [ "$first" -eq 0 ]; then
      files_json+=","
    fi
    files_json+="
    {\"path\":\"$rel\",\"sha256\":\"$hash\",\"size\":$size}"
    first=0
  done
  cat > "$package_root/acbh-package.json" <<EOF
{
  "version": 1,
  "id": "acbh-runtime-base-windows-amd64-$version",
  "packageId": "acbh-runtime-base-windows-amd64",
  "kind": "runtime-base",
  "os": "windows",
  "architecture": "amd64",
  "signature": "sha256-manifest-placeholder-$version",
  "files": [$files_json
  ]
}
EOF
  zip_dir "$package_root" "$target_zip"
}

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

echo ""
echo "=== Building coordinator dist ==="
if command -v pnpm >/dev/null 2>&1; then
  (cd "$REPO_ROOT" && pnpm --filter @acbh/coordinator build)
elif command -v corepack >/dev/null 2>&1; then
  (cd "$REPO_ROOT" && corepack pnpm --filter @acbh/coordinator build)
else
  echo "pnpm/corepack not found; cannot build coordinator dist" >&2
  exit 1
fi

if [ -f "$DIST_DIR/acbh-desktop-windows-amd64.exe" ]; then
  echo ""
  echo "=== Building private desktop bundle ==="
  BUNDLE_NAME="acbh-${VERSION}-bundle"
  BUNDLE_PARENT="$(mktemp -d)"
  BUNDLE_ROOT="$BUNDLE_PARENT/$BUNDLE_NAME"
  rm -rf "$DIST_DIR/$BUNDLE_NAME" "$DIST_DIR/$BUNDLE_NAME.zip"
  mkdir -p "$BUNDLE_ROOT/scripts"
  mkdir -p "$BUNDLE_ROOT/docs"
  mkdir -p "$BUNDLE_ROOT/resources"
  cp "$DIST_DIR/acbh-desktop-windows-amd64.exe" "$BUNDLE_ROOT/"
  cp "$DIST_DIR/acbh-agent-windows-amd64.exe" "$BUNDLE_ROOT/"
  copy_powershell_utf8_bom "$REPO_ROOT/scripts/acbh-desktop-gui.ps1" "$BUNDLE_ROOT/scripts/acbh-desktop-gui.ps1"
  cp -R "$REPO_ROOT/docs/zh-CN" "$BUNDLE_ROOT/docs/"
  cp "$REPO_ROOT/RELEASE_NOTES_${VERSION}.md" "$BUNDLE_ROOT/" 2>/dev/null || \
    cp "$REPO_ROOT/RELEASE_NOTES_v0.3.3-simple-desktop-flow.md" "$BUNDLE_ROOT/" 2>/dev/null || true
  printf "ACBH runtime base package for %s\n" "$VERSION" > "$BUNDLE_ROOT/resources/runtime-base.README.txt"
  copy_private_node_runtime "$BUNDLE_ROOT"
  mkdir -p "$BUNDLE_ROOT/coordinator/dist"
  cp "$REPO_ROOT/apps/coordinator/package.json" "$BUNDLE_ROOT/coordinator/"
  cp -R "$REPO_ROOT/apps/coordinator/dist/." "$BUNDLE_ROOT/coordinator/dist/"
  if command -v npm >/dev/null 2>&1; then
    (cd "$BUNDLE_ROOT/coordinator" && npm install --omit=dev --no-audit --no-fund --package-lock=false)
  else
    echo "npm not found; cannot build self-contained coordinator dependencies" >&2
    exit 1
  fi

  ENV_PACKAGE_ROOT="$BUNDLE_PARENT/acbh-runtime-base-windows-amd64"
  mkdir -p "$ENV_PACKAGE_ROOT/node" "$ENV_PACKAGE_ROOT/resources"
  cp "$BUNDLE_ROOT/runtime/node/node.exe" "$ENV_PACKAGE_ROOT/node/node.exe"
  cp "$BUNDLE_ROOT/resources/runtime-base.README.txt" "$ENV_PACKAGE_ROOT/resources/runtime-base.README.txt"
  build_runtime_package "$ENV_PACKAGE_ROOT" "$DIST_DIR/acbh-runtime-base-windows-amd64.zip" "$VERSION"

  cp -R "$BUNDLE_ROOT" "$DIST_DIR/"
  zip_dir "$BUNDLE_ROOT" "$DIST_DIR/$BUNDLE_NAME.zip"

  COORD_BUNDLE_NAME="acbh-coordinator-linux-amd64-bundle"
  COORD_BUNDLE_ROOT="$BUNDLE_PARENT/$COORD_BUNDLE_NAME"
  mkdir -p "$COORD_BUNDLE_ROOT/dist"
  mkdir -p "$COORD_BUNDLE_ROOT/packages"
  mkdir -p "$COORD_BUNDLE_ROOT/scripts"
  cp "$REPO_ROOT/apps/coordinator/package.json" "$COORD_BUNDLE_ROOT/"
  cp -R "$REPO_ROOT/apps/coordinator/dist/." "$COORD_BUNDLE_ROOT/dist/"
  cp "$REPO_ROOT/scripts/acbh-vps-lib.sh" "$COORD_BUNDLE_ROOT/scripts/"
  cp "$REPO_ROOT/scripts/install-acbh-vps.sh" "$COORD_BUNDLE_ROOT/scripts/"
  cp "$REPO_ROOT/scripts/acbh-vps-status.sh" "$COORD_BUNDLE_ROOT/scripts/"
  cp "$REPO_ROOT/scripts/acbh-vps-upgrade.sh" "$COORD_BUNDLE_ROOT/scripts/"
  cp "$REPO_ROOT/scripts/acbh-vps-rollback.sh" "$COORD_BUNDLE_ROOT/scripts/"
  chmod +x "$COORD_BUNDLE_ROOT/scripts/"*.sh
  printf '%s\n' "$VERSION" > "$COORD_BUNDLE_ROOT/VERSION"
  cp "$DIST_DIR/acbh-runtime-base-windows-amd64.zip" "$COORD_BUNDLE_ROOT/packages/acbh-runtime-base-windows-amd64.zip"
  cat > "$COORD_BUNDLE_ROOT/run-coordinator.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec node "$DIR/dist/index.js"
EOF
  chmod +x "$COORD_BUNDLE_ROOT/run-coordinator.sh"
  if command -v npm >/dev/null 2>&1; then
    (cd "$COORD_BUNDLE_ROOT" && npm install --omit=dev --no-audit --no-fund --package-lock=false)
  else
    echo "npm not found; cannot build coordinator linux bundle dependencies" >&2
    exit 1
  fi
  (
    cd "$COORD_BUNDLE_ROOT"
    if command -v sha256sum >/dev/null 2>&1; then
      find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
    else
      find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 shasum -a 256 > SHA256SUMS
    fi
  )
  tar -C "$BUNDLE_PARENT" -czf "$DIST_DIR/$COORD_BUNDLE_NAME.tar.gz" "$COORD_BUNDLE_NAME"

  rm -rf "$BUNDLE_PARENT"
  echo "  $DIST_DIR/$BUNDLE_NAME.zip"
  echo "  $DIST_DIR/acbh-runtime-base-windows-amd64.zip"
  echo "  $DIST_DIR/$COORD_BUNDLE_NAME.tar.gz"
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
