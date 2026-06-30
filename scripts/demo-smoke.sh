#!/usr/bin/env bash
# Minimal closed-loop demo without Minecraft, public networking, or cloud services.
set -euo pipefail
set +x

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC}  $*"; }
CURRENT_STAGE="initialization"
fail() {
  echo -e "${RED}[FAIL]${NC}  stage=${CURRENT_STAGE}: $*"
  exit 1
}

umask 077
DEMO_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/acbh-demo.XXXXXX")
COORD_PID=""
COORD_STOP_MODE=""
COORD_LOG="$DEMO_TMPDIR/coordinator.log"
COORDINATOR_PORT=""

run_timed() {
  local duration="$1"
  shift
  timeout --foreground --kill-after=2s "$duration" "$@"
}

stop_coordinator() {
  [ -n "$COORD_PID" ] || return 0

  case "$COORD_STOP_MODE" in
    process-group)
      kill -TERM -- "-$COORD_PID" 2>/dev/null || true
      for _ in $(seq 1 20); do
        if ! kill -0 -- "-$COORD_PID" 2>/dev/null; then
          COORD_PID=""
          return 0
        fi
        sleep 0.1
      done
      warn "Coordinator process group did not stop after TERM; sending KILL"
      kill -KILL -- "-$COORD_PID" 2>/dev/null || true
      ;;
    windows-tree)
      run_timed 5s env MSYS_NO_PATHCONV=1 taskkill.exe /PID "$COORD_PID" /T /F >/dev/null 2>&1 || true
      if [ -n "$COORDINATOR_PORT" ]; then
        local listener_pid
        listener_pid=$(run_timed 5s powershell.exe -NoProfile -NonInteractive -Command \
          "(Get-NetTCPConnection -LocalPort $COORDINATOR_PORT -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty OwningProcess)" |
          tr -d '\r[:space:]') || true
        if [[ "$listener_pid" =~ ^[0-9]+$ ]]; then
          if ! run_timed 5s env MSYS_NO_PATHCONV=1 taskkill.exe /PID "$listener_pid" /T /F >/dev/null 2>&1; then
            warn "Coordinator listener process-tree cleanup reported a failure"
          fi
        fi
      fi
      ;;
    *)
      kill -TERM "$COORD_PID" 2>/dev/null || true
      for _ in $(seq 1 20); do
        if ! kill -0 "$COORD_PID" 2>/dev/null; then
          COORD_PID=""
          return 0
        fi
        sleep 0.1
      done
      warn "Coordinator process did not stop after TERM; sending KILL"
      kill -KILL "$COORD_PID" 2>/dev/null || true
      ;;
  esac

  COORD_PID=""
}

cleanup() {
  local status="$1"
  trap - ERR EXIT INT TERM
  CURRENT_STAGE="cleanup"

  stop_coordinator

  if [ -n "$COORDINATOR_PORT" ] &&
    curl --connect-timeout 1 --max-time 2 -sf "http://127.0.0.1:$COORDINATOR_PORT/health" >/dev/null 2>&1; then
    warn "Coordinator still responds on demo port $COORDINATOR_PORT after cleanup"
  fi

  if [ "$status" -ne 0 ] && [ -s "$COORD_LOG" ]; then
    warn "Coordinator log: $COORD_LOG"
    warn "Coordinator log tail:"
    tail -n 20 "$COORD_LOG" || true
  fi

  if ! run_timed 5s rm -rf "$DEMO_TMPDIR"; then
    warn "Failed to remove demo temporary directory: $DEMO_TMPDIR"
  fi
  exit "$status"
}

on_error() {
  local status=$?
  warn "Stage failed: $CURRENT_STAGE (exit $status)"
  return "$status"
}

trap on_error ERR
trap 'cleanup "$?"' EXIT
trap 'cleanup 130' INT
trap 'cleanup 143' TERM

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

resolve_pnpm() {
  if command -v pnpm >/dev/null 2>&1; then
    PNPM=(pnpm)
  elif command -v corepack >/dev/null 2>&1; then
    PNPM=(corepack pnpm)
  else
    fail "pnpm not found; install pnpm or enable Corepack"
  fi
}

curl_auth() {
  local method="$1" url="$2" host_id="$3" host_token="$4" access_key="$5" data="$6"
  local header_file body_file
  header_file=$(mktemp "$DEMO_TMPDIR/curl-headers.XXXXXX")
  {
    echo "Content-Type: application/json"
    if [ -n "$host_id" ]; then
      echo "x-acbh-host-id: $host_id"
      echo "x-acbh-host-token: $host_token"
    fi
    if [ -n "$access_key" ]; then
      echo "x-acbh-access-key: $access_key"
    fi
  } > "$header_file"

  if [ -n "$data" ]; then
    body_file=$(mktemp "$DEMO_TMPDIR/curl-body.XXXXXX")
    printf '%s\n' "$data" > "$body_file"
    curl --connect-timeout 3 --max-time 10 -sf -X "$method" "$url" -H "@$header_file" --data-binary "@$body_file"
    rm -f "$body_file"
  else
    curl --connect-timeout 3 --max-time 10 -sf -X "$method" "$url" -H "@$header_file"
  fi
  rm -f "$header_file"
}

json_request() {
  curl_auth "$1" "$2" "" "" "" "$3"
}

json_field() {
  local field="$1"
  run_timed 10s node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const v=JSON.parse(s)[process.argv[1]];if(typeof v!=="string")process.exit(2);process.stdout.write(v)})' "$field"
}

choose_port() {
  run_timed 10s node -e 'const net=require("net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})'
}

start_coordinator() {
  : > "$COORD_LOG"
  if command -v setsid >/dev/null 2>&1; then
    setsid "${PNPM[@]}" --filter @acbh/coordinator start >"$COORD_LOG" 2>&1 &
    COORD_STOP_MODE="process-group"
  elif [[ "${OSTYPE:-}" == msys* || "${OSTYPE:-}" == cygwin* ]]; then
    "${PNPM[@]}" --filter @acbh/coordinator start >"$COORD_LOG" 2>&1 &
    COORD_STOP_MODE="windows-tree"
  else
    "${PNPM[@]}" --filter @acbh/coordinator start >"$COORD_LOG" 2>&1 &
    COORD_STOP_MODE="single-process"
  fi
  COORD_PID=$!
}

require_command curl
require_command go
require_command node
require_command timeout
resolve_pnpm

export XDG_CONFIG_HOME="$DEMO_TMPDIR/xdg"
export APPDATA="$DEMO_TMPDIR/appdata"
export LOCALAPPDATA="$DEMO_TMPDIR/localappdata"
export ACBH_STORAGE_ROOT="$DEMO_TMPDIR/storage"
export ACBH_COORDINATOR_STATE_PATH="$DEMO_TMPDIR/coordinator-state.json"
export HOST="127.0.0.1"

info "ACBH Demo Smoke"
CURRENT_STAGE="build coordinator"
info "Building Coordinator..."
run_timed 120s "${PNPM[@]}" --filter @acbh/coordinator build || fail "Coordinator build failed or timed out"

CURRENT_STAGE="build agent"
info "Building Agent..."
GOOS=$(run_timed 10s go env GOOS) || fail "Unable to determine Go target OS"
AGENT_SUFFIX=""
if [ "$GOOS" = "windows" ]; then
  AGENT_SUFFIX=".exe"
fi
AGENT="$DEMO_TMPDIR/acbh-agent$AGENT_SUFFIX"
(cd agent && run_timed 120s go build -o "$AGENT" .) || fail "Agent build failed or timed out"

CURRENT_STAGE="start coordinator"
ready=false
for attempt in 1 2 3; do
  COORDINATOR_PORT=$(choose_port)
  export PORT="$COORDINATOR_PORT"
  info "Starting Coordinator on 127.0.0.1:$COORDINATOR_PORT (attempt $attempt/3)..."
  start_coordinator
  for _ in $(seq 1 30); do
    if curl --connect-timeout 3 --max-time 10 -sf "http://127.0.0.1:$COORDINATOR_PORT/health" >/dev/null 2>&1; then
      ready=true
      break
    fi
    if ! kill -0 "$COORD_PID" 2>/dev/null; then
      break
    fi
    sleep 0.5
  done
  if [ "$ready" = true ]; then
    break
  fi
  stop_coordinator
done
[ "$ready" = true ] || fail "Coordinator failed to start within 15s; log: $COORD_LOG"
COORDINATOR_URL="http://127.0.0.1:$COORDINATOR_PORT"

CURRENT_STAGE="coordinator health"
info "Checking Coordinator health..."
HEALTH=$(curl --connect-timeout 3 --max-time 10 -sf "$COORDINATOR_URL/health") || fail "Coordinator health check failed"
echo "$HEALTH" | grep -q '"ok":true' || fail "Coordinator not healthy"
info "Coordinator is healthy"

CURRENT_STAGE="create group"
info "Creating group..."
GROUP=$(json_request POST "$COORDINATOR_URL/v1/groups" '{"name":"Demo Group","ownerName":"Demo Owner"}')
GROUP_ID=$(printf '%s' "$GROUP" | json_field groupId)
ACCESS_KEY=$(printf '%s' "$GROUP" | json_field accessKey)
info "Group created: $GROUP_ID"

CURRENT_STAGE="register host"
info "Registering host..."
MEMBER=$(json_request POST "$COORDINATOR_URL/v1/groups/$GROUP_ID/join" \
  "{\"accessKey\":\"$ACCESS_KEY\",\"displayName\":\"Demo Host\"}")
MEMBER_ID=$(printf '%s' "$MEMBER" | json_field memberId)
HOST_RESPONSE=$(json_request POST "$COORDINATOR_URL/v1/hosts/register" \
  "{\"groupId\":\"$GROUP_ID\",\"accessKey\":\"$ACCESS_KEY\",\"memberId\":\"$MEMBER_ID\",\"deviceName\":\"demo-device\",\"platform\":\"$GOOS\",\"agentVersion\":\"0.1.0\"}")
HOST_ID=$(printf '%s' "$HOST_RESPONSE" | json_field hostId)
HOST_TOKEN=$(printf '%s' "$HOST_RESPONSE" | json_field hostToken)
info "Host registered: $HOST_ID"

if [ "$GOOS" = "windows" ]; then
  AGENT_CONFIG_DIR="$APPDATA/acbh"
else
  AGENT_CONFIG_DIR="$XDG_CONFIG_HOME/acbh"
fi
mkdir -p "$AGENT_CONFIG_DIR"
cat > "$AGENT_CONFIG_DIR/config.yaml" <<EOF
{
  "coordinatorUrl": "$COORDINATOR_URL",
  "groupId": "$GROUP_ID",
  "memberId": "$MEMBER_ID",
  "hostId": "$HOST_ID",
  "hostToken": "$HOST_TOKEN",
  "displayName": "Demo Host",
  "deviceName": "demo-device",
  "platform": "$GOOS",
  "agentVersion": "0.1.0"
}
EOF

CURRENT_STAGE="heartbeat"
info "Sending heartbeat..."
HEARTBEAT=$(json_request POST "$COORDINATOR_URL/v1/hosts/heartbeat" \
  "{\"groupId\":\"$GROUP_ID\",\"hostId\":\"$HOST_ID\",\"hostToken\":\"$HOST_TOKEN\",\"status\":\"standby\"}")
echo "$HEARTBEAT" | grep -q '"ok":true' || fail "Heartbeat failed"
info "Heartbeat OK"

CURRENT_STAGE="scan manifest"
info "Creating fake server directory and scanning..."
FAKE_SERVER="$DEMO_TMPDIR/fake-server"
mkdir -p "$FAKE_SERVER/world/region"
echo "world-data" > "$FAKE_SERVER/world/region/r.0.0.mca"
MANIFEST_PATH="$DEMO_TMPDIR/manifest.json"
run_timed 30s "$AGENT" scan \
  --server-dir "$FAKE_SERVER" \
  --artifact-kind world-snapshot \
  --artifact-id snap_demo_001 \
  --server-pack-version pack_demo_001 \
  --group-id "$GROUP_ID" \
  --creator-host-id "$HOST_ID" \
  --output "$MANIFEST_PATH" || fail "Scan failed"

CURRENT_STAGE="validate manifest"
info "Validating manifest..."
run_timed 30s "$AGENT" manifest validate --file "$MANIFEST_PATH" || fail "Manifest validation failed or timed out"
info "Manifest is valid"

CURRENT_STAGE="push artifact"
info "Pushing artifact to Coordinator..."
run_timed 30s "$AGENT" push --manifest "$MANIFEST_PATH" --server-dir "$FAKE_SERVER" || fail "Push failed or timed out"
info "Push complete"

CURRENT_STAGE="query latest artifact"
info "Checking latest artifact..."
LATEST=$(curl_auth GET "$COORDINATOR_URL/v1/groups/$GROUP_ID/artifacts/latest?artifactKind=world-snapshot" "$HOST_ID" "$HOST_TOKEN" "" "")
echo "$LATEST" | grep -q '"artifactId":"snap_demo_001"' || fail "Latest artifact not found"
info "Latest artifact confirmed"

CURRENT_STAGE="pull artifact"
info "Pulling artifact to restore directory..."
RESTORE_DIR="$DEMO_TMPDIR/restore"
run_timed 30s "$AGENT" pull \
  --artifact-kind world-snapshot \
  --artifact-id snap_demo_001 \
  --output-dir "$RESTORE_DIR" || fail "Pull failed or timed out"
[ -f "$RESTORE_DIR/world/region/r.0.0.mca" ] || fail "Restored file not found"
info "Restored file confirmed"

CURRENT_STAGE="check group state"
info "Checking group state..."
STATE=$(curl_auth GET "$COORDINATOR_URL/v1/groups/$GROUP_ID/state" "" "" "$ACCESS_KEY" "")
echo "$STATE" | grep -q "$GROUP_ID" || fail "Group state check failed"
if echo "$STATE" | grep -q "$ACCESS_KEY"; then
  fail "Group state leaked access key"
fi
if echo "$STATE" | grep -q "$HOST_TOKEN"; then
  fail "Group state leaked host token"
fi
info "Group state OK; no secret leaked"

CURRENT_STAGE="local control"
info "Local Control: SKIP (not required for the closed-loop artifact smoke test)"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ACBH Demo Smoke: PASS${NC}"
echo -e "${GREEN}========================================${NC}"
