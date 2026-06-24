#!/usr/bin/env bash
set -euo pipefail

PUBLIC_HOST=""
INSTALL_DIR="/opt/acbh"
COORDINATOR_PORT="6121"
RELAY_PORT="25565"
CONFIGURE_UFW="false"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --public-host) PUBLIC_HOST="${2:-}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:-}"; shift 2 ;;
    --coordinator-port) COORDINATOR_PORT="${2:-}"; shift 2 ;;
    --relay-port) RELAY_PORT="${2:-}"; shift 2 ;;
    --configure-ufw) CONFIGURE_UFW="true"; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "please run with sudo/root" >&2
  exit 1
fi
if [ -z "$PUBLIC_HOST" ]; then
  echo "--public-host is required" >&2
  exit 2
fi

. /etc/os-release
if [ "${ID:-}" != "ubuntu" ] || { [ "${VERSION_ID:-}" != "22.04" ] && [ "${VERSION_ID:-}" != "24.04" ]; }; then
  echo "ACBH VPS installer supports Ubuntu 22.04/24.04 only" >&2
  exit 1
fi
if [ "$(uname -m)" != "x86_64" ]; then
  echo "ACBH VPS installer supports x86_64 only" >&2
  exit 1
fi

if ! id acbh >/dev/null 2>&1; then
  useradd --system --home "$INSTALL_DIR" --shell /usr/sbin/nologin acbh
fi

mkdir -p "$INSTALL_DIR"/{storage,data,logs,packages,dist}
chown -R acbh:acbh "$INSTALL_DIR"

if ! command -v node >/dev/null 2>&1; then
  apt-get update
  apt-get install -y ca-certificates curl gnupg
  curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
  apt-get install -y nodejs
fi

NODE_MAJOR="$(node -p 'process.versions.node.split(".")[0]')"
if [ "$NODE_MAJOR" -lt 20 ]; then
  echo "Node.js 20+ LTS is required; current node is $(node --version)" >&2
  exit 1
fi

cat > "$INSTALL_DIR/.env" <<EOF
HOST=0.0.0.0
PORT=$COORDINATOR_PORT
ACBH_PUBLIC_HOST=$PUBLIC_HOST
ACBH_RELAY_PUBLIC_HOST=0.0.0.0
ACBH_RELAY_PUBLIC_PORT=$RELAY_PORT
ACBH_STORAGE_ROOT=$INSTALL_DIR/storage
ACBH_COORDINATOR_STATE_PATH=$INSTALL_DIR/data/coordinator-state.json
ACBH_BOOTSTRAP_PACKAGE_DIR=$INSTALL_DIR/packages
ACBH_ARTIFACT_RETENTION_PER_KIND=3
ACBH_GC_MIN_AGE_MS=3600000
ACBH_TUNNEL_SESSION_TTL_MS=300000
ACBH_MAX_OBJECT_BYTES=268435456
EOF
chown acbh:acbh "$INSTALL_DIR/.env"
chmod 600 "$INSTALL_DIR/.env"

cat > /etc/systemd/system/acbh-coordinator.service <<EOF
[Unit]
Description=ACBH Coordinator
After=network-online.target
Wants=network-online.target

[Service]
User=acbh
Group=acbh
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/.env
ExecStart=/usr/bin/node $INSTALL_DIR/dist/index.js
Restart=always
RestartSec=3
StandardOutput=append:$INSTALL_DIR/logs/coordinator.log
StandardError=append:$INSTALL_DIR/logs/coordinator.log

[Install]
WantedBy=multi-user.target
EOF

if [ "$CONFIGURE_UFW" = "true" ]; then
  if command -v ufw >/dev/null 2>&1; then
    ufw allow "$COORDINATOR_PORT"/tcp
    ufw allow "$RELAY_PORT"/tcp
  else
    echo "ufw not installed; skipping firewall configuration"
  fi
else
  echo "UFW not changed. Re-run with --configure-ufw to open $COORDINATOR_PORT/tcp and $RELAY_PORT/tcp."
fi

systemctl daemon-reload
systemctl enable acbh-coordinator
systemctl restart acbh-coordinator || true

sleep 2
if ss -ltn | grep -q ":$COORDINATOR_PORT "; then
  echo "Coordinator port $COORDINATOR_PORT is listening."
else
  echo "Warning: Coordinator port $COORDINATOR_PORT is not listening yet."
fi
if ss -ltn | grep -q ":$RELAY_PORT "; then
  echo "Relay/player port $RELAY_PORT is listening."
else
  echo "Warning: relay/player port $RELAY_PORT is not listening yet."
fi
curl -fsS "http://127.0.0.1:$COORDINATOR_PORT/health" >/dev/null && echo "/health ok" || echo "Warning: /health failed"
curl -fsS "http://127.0.0.1:$COORDINATOR_PORT/v1/bootstrap/manifest" >/dev/null && echo "bootstrap manifest ok" || echo "Warning: bootstrap manifest failed"

echo
echo "Windows 端公网服务器 IP 或域名填写："
echo "$PUBLIC_HOST"
