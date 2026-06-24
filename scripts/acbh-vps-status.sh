#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./acbh-vps-lib.sh
. "$SCRIPT_DIR/acbh-vps-lib.sh"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir) ACBH_INSTALL_DIR="${2:-}"; shift 2 ;;
    -h|--help)
      cat <<'EOF'
usage: bash acbh-vps-status.sh [--install-dir /opt/acbh]
EOF
      exit 0
      ;;
    *)
      acbh_vps_die "unknown argument: $1"
      ;;
  esac
done

echo "ACBH install dir: $ACBH_INSTALL_DIR"

if current_release="$(acbh_vps_current_release_dir 2>/dev/null || true)"; then
  current_version="$(acbh_vps_release_version_path "$current_release")"
  echo "Current release: $current_version"
  echo "Current path:    $current_release"
else
  echo "Current release: (none)"
fi

if [ -f "$(acbh_vps_upgrade_state_path)" ]; then
  echo "Upgrade state:"
  cat "$(acbh_vps_upgrade_state_path)"
  echo
fi

echo "Service status:"
"$ACBH_SYSTEMCTL_CMD" --no-pager --full status "$ACBH_SYSTEMD_SERVICE" || true
echo
echo "Listening ports:"
"$ACBH_SS_CMD" -ltn | grep -E ":(${ACBH_COORDINATOR_PORT}|${ACBH_RELAY_PORT}) " || true
echo
echo "Health:"
"$ACBH_CURL_CMD" -fsS "http://127.0.0.1:${ACBH_COORDINATOR_PORT}/health" && echo || true
echo "Bootstrap manifest:"
"$ACBH_CURL_CMD" -fsS "http://127.0.0.1:${ACBH_COORDINATOR_PORT}/v1/bootstrap/manifest" && echo || true