#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=acbh-vps-lib.sh
. "$SCRIPT_DIR/acbh-vps-lib.sh"

PUBLIC_HOST=""
CONFIGURE_UFW="false"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --public-host) PUBLIC_HOST="${2:-}"; shift 2 ;;
    --install-dir) ACBH_INSTALL_DIR="${2:-}"; shift 2 ;;
    --coordinator-port) ACBH_COORDINATOR_PORT="${2:-}"; shift 2 ;;
    --relay-port) ACBH_RELAY_PORT="${2:-}"; shift 2 ;;
    --configure-ufw) CONFIGURE_UFW="true"; shift ;;
    *) acbh_vps_die "unknown argument: $1" ;;
  esac
done

acbh_vps_require_root

if [ -z "$PUBLIC_HOST" ]; then
  acbh_vps_die "--public-host is required"
fi

if [ "$ACBH_SKIP_ROOT" != "1" ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  if [ "${ID:-}" != "ubuntu" ] || { [ "${VERSION_ID:-}" != "22.04" ] && [ "${VERSION_ID:-}" != "24.04" ]; }; then
    acbh_vps_die "ACBH VPS installer supports Ubuntu 22.04/24.04 only"
  fi
  if [ "$(uname -m)" != "x86_64" ]; then
    acbh_vps_die "ACBH VPS installer supports x86_64 only"
  fi
fi

if [ "$ACBH_SKIP_ROOT" != "1" ] && ! id acbh >/dev/null 2>&1; then
  useradd --system --home "$ACBH_INSTALL_DIR" --shell /usr/sbin/nologin acbh
fi

acbh_vps_ensure_layout

if [ "$ACBH_SKIP_ROOT" != "1" ] && ! command -v node >/dev/null 2>&1; then
  apt-get update
  apt-get install -y ca-certificates curl gnupg rsync
  curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
  apt-get install -y nodejs
fi

acbh_vps_validate_node_version
acbh_vps_migrate_legacy_layout

if [ ! -f "$ACBH_INSTALL_DIR/.env" ]; then
  cat > "$ACBH_INSTALL_DIR/.env" <<EOF
HOST=0.0.0.0
PORT=$ACBH_COORDINATOR_PORT
ACBH_PUBLIC_HOST=$PUBLIC_HOST
ACBH_RELAY_PUBLIC_HOST=0.0.0.0
ACBH_RELAY_PUBLIC_PORT=$ACBH_RELAY_PORT
ACBH_STORAGE_ROOT=$ACBH_INSTALL_DIR/storage
ACBH_COORDINATOR_STATE_PATH=$ACBH_INSTALL_DIR/data/coordinator-state.json
ACBH_BOOTSTRAP_PACKAGE_DIR=$ACBH_INSTALL_DIR/packages
ACBH_ARTIFACT_RETENTION_PER_KIND=3
ACBH_GC_MIN_AGE_MS=3600000
ACBH_TUNNEL_SESSION_TTL_MS=300000
ACBH_MAX_OBJECT_BYTES=268435456
EOF
  if [ "$ACBH_SKIP_ROOT" != "1" ]; then
    chown acbh:acbh "$ACBH_INSTALL_DIR/.env"
    chmod 600 "$ACBH_INSTALL_DIR/.env"
  fi
else
  acbh_vps_log "preserving existing $ACBH_INSTALL_DIR/.env"
fi

if [ "$ACBH_SKIP_ROOT" != "1" ]; then
  chown -R acbh:acbh \
    "$ACBH_INSTALL_DIR/storage" \
    "$ACBH_INSTALL_DIR/data" \
    "$ACBH_INSTALL_DIR/logs" \
    "$ACBH_INSTALL_DIR/packages" \
    "$(acbh_vps_releases_dir)"
fi

acbh_vps_write_systemd_unit
acbh_vps_service_enable

if [ -L "$(acbh_vps_current_link)" ] || [ -d "$(acbh_vps_current_link)" ]; then
  acbh_vps_service_restart || true
else
  acbh_vps_log "no current release yet; deploy a bundle with acbh-vps-upgrade.sh"
fi

if [ "$CONFIGURE_UFW" = "true" ] && [ "$ACBH_SKIP_ROOT" != "1" ]; then
  if command -v ufw >/dev/null 2>&1; then
    ufw allow "${ACBH_COORDINATOR_PORT}/tcp"
    ufw allow "${ACBH_RELAY_PORT}/tcp"
  else
    acbh_vps_log "ufw not installed; skipping firewall configuration"
  fi
else
  acbh_vps_log "UFW not changed. Re-run with --configure-ufw to open ${ACBH_COORDINATOR_PORT}/tcp and ${ACBH_RELAY_PORT}/tcp."
fi

sleep 2
if "$ACBH_SS_CMD" -ltn | grep -q ":${ACBH_COORDINATOR_PORT} "; then
  acbh_vps_log "Coordinator port $ACBH_COORDINATOR_PORT is listening."
else
  acbh_vps_log "Warning: Coordinator port $ACBH_COORDINATOR_PORT is not listening yet."
fi
if "$ACBH_SS_CMD" -ltn | grep -q ":${ACBH_RELAY_PORT} "; then
  acbh_vps_log "Relay/player port $ACBH_RELAY_PORT is listening."
else
  acbh_vps_log "Warning: relay/player port $ACBH_RELAY_PORT is not listening yet."
fi

if [ -L "$(acbh_vps_current_link)" ] || [ -d "$(acbh_vps_current_link)" ]; then
  "$ACBH_CURL_CMD" -fsS "http://127.0.0.1:${ACBH_COORDINATOR_PORT}/health" >/dev/null && acbh_vps_log "/health ok" || acbh_vps_log "Warning: /health failed"
  "$ACBH_CURL_CMD" -fsS "http://127.0.0.1:${ACBH_COORDINATOR_PORT}/v1/bootstrap/manifest" >/dev/null && acbh_vps_log "bootstrap manifest ok" || acbh_vps_log "Warning: bootstrap manifest failed"
fi

echo
echo "Windows 端公网服务器 IP 或域名填写："
echo "$PUBLIC_HOST"