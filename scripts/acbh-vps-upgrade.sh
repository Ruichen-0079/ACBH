#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/acbh}"
SOURCE_DIR="${1:-}"

if [ "$(id -u)" -ne 0 ]; then
  echo "please run with sudo/root" >&2
  exit 1
fi
if [ -z "$SOURCE_DIR" ]; then
  echo "usage: sudo bash acbh-vps-upgrade.sh /path/to/unpacked/coordinator-bundle" >&2
  exit 2
fi
if [ ! -d "$SOURCE_DIR/dist" ]; then
  echo "source bundle must contain dist/" >&2
  exit 2
fi

mkdir -p "$INSTALL_DIR"/{storage,data,logs,packages,dist}
rsync -a --delete "$SOURCE_DIR/dist/" "$INSTALL_DIR/dist/"
if [ -f "$SOURCE_DIR/package.json" ]; then
  cp "$SOURCE_DIR/package.json" "$INSTALL_DIR/package.json"
fi
if [ -d "$SOURCE_DIR/node_modules" ]; then
  rsync -a --delete "$SOURCE_DIR/node_modules/" "$INSTALL_DIR/node_modules/"
fi
if [ -d "$SOURCE_DIR/packages" ]; then
  rsync -a "$SOURCE_DIR/packages/" "$INSTALL_DIR/packages/"
fi
chown -R acbh:acbh "$INSTALL_DIR"
systemctl restart acbh-coordinator
systemctl --no-pager --full status acbh-coordinator || true
