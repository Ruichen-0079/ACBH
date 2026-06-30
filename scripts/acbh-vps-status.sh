#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/acbh}"
PORT="${ACBH_COORDINATOR_PORT:-6121}"

echo "ACBH install dir: $INSTALL_DIR"
systemctl --no-pager --full status acbh-coordinator || true
echo
echo "Listening ports:"
ss -ltn | grep -E ":($PORT|25565) " || true
echo
curl -fsS "http://127.0.0.1:$PORT/health" && echo
curl -fsS "http://127.0.0.1:$PORT/v1/bootstrap/manifest" && echo
