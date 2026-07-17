#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"

"$SCRIPT_DIR/build-agent-release.sh"

zip_bundle_dir() {
  local source_dir="$1"
  local output_zip="$2"
  if command -v zip >/dev/null 2>&1; then
    (cd "$source_dir" && zip -qr "$output_zip" .)
    return
  fi
  if command -v powershell.exe >/dev/null 2>&1; then
    local win_source="$source_dir"
    local win_output="$output_zip"
    if command -v cygpath >/dev/null 2>&1; then
      win_source="$(cygpath -w "$source_dir")"
      win_output="$(cygpath -w "$output_zip")"
    fi
    powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command \
      "\$ErrorActionPreference='Stop'; \$src='$win_source'; \$dst='$win_output'; Compress-Archive -Path (Join-Path \$src '*') -DestinationPath \$dst -Force"
    return
  fi
  echo "zip command not found and powershell.exe fallback is unavailable" >&2
  return 1
}

HOTFIX_PANEL="$REPO_ROOT/scripts/acbh-desktop-gui-v035.ps1"
if [ ! -f "$HOTFIX_PANEL" ]; then
  echo "missing hotfix panel: $HOTFIX_PANEL" >&2
  exit 1
fi

shopt -s nullglob
for bundle_dir in "$DIST_DIR"/acbh-*-bundle; do
  [ -d "$bundle_dir" ] || continue
  panel_dir="$bundle_dir/scripts"
  mkdir -p "$panel_dir"
  if [ -f "$panel_dir/acbh-desktop-gui.ps1" ]; then
    cp "$panel_dir/acbh-desktop-gui.ps1" "$panel_dir/acbh-desktop-gui-legacy.ps1"
  fi
  cp "$HOTFIX_PANEL" "$panel_dir/acbh-desktop-gui.ps1"

  bundle_name="$(basename "$bundle_dir")"
  bundle_zip="$DIST_DIR/$bundle_name.zip"
  rm -f "$bundle_zip"
  zip_bundle_dir "$bundle_dir" "$bundle_zip"
  echo "hotfix panel packaged: $bundle_zip"
done

(
  cd "$DIST_DIR"
  find . -maxdepth 1 -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)
