#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"

"$SCRIPT_DIR/build-agent-release.sh"

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
  (cd "$bundle_dir" && zip -qr "$bundle_zip" .)
  echo "hotfix panel packaged: $bundle_zip"
done

(
  cd "$DIST_DIR"
  find . -maxdepth 1 -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)
