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
fail() { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

umask 077
DEMO_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/acbh-demo.XXXXXX")
COORD_PID=""
COORD_LOG="$DEMO_TMPDIR/coordinator.log"

cleanup() {
  local status="$1"
  trap - EXIT INT TERM
  if [ -n "$COORD_PID" ] && kill -0 "$COORD_PID" 2>/dev/null; then
    kill "$COORD_PID" 2>/dev/null || true
    wait "$COORD_PID" 2>/dev/null || true
  fi
  if [ "$status" -ne 0 ] && [ -s "$COORD_LOG" ]; then
    warn "Coordinator log tail:"
    tail -n 20 "$COORD_LOG" || true
  fi
  rm -rf "$DEMO_TMPDIR"
  exit "$status"
}
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
    curl -sf -X "$method" "$url" -H "@$header_file" --data-binary "@$body_file"
    rm -f "$body_file"
  else
    curl -sf -X "$method" "$url" -H "@$header_file"
  fi
  rm -f "$header_file"
}

json_request() {
  curl_auth "$1" "$2" "" "" "" "$3"
}

json_field() {
  local field="$1"
  node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const v=JSON.parse(s)[process.argv[1]];if(typeof v!=="string")process.exit(2);process.stdout.write(v)})' "$field"
}

choose_port() {
  node -e 'const net=require("net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})'
}

require_command curl
require_command go
require_command node
resolve_pnpm

export XDG_CONFIG_HOME="$DEMO_TMPDIR/xdg"
export APPDATA="$DEMO_TMPDIR/appdata"
export LOCALAPPDATA="$DEMO_TMPDIR/localappdata"
export ACBH_STORAGE_ROOT="$DEMO_TMPDIR/storage"
export ACBH_COORDINATOR_STATE_PATH="$DEMO_TMPDIR/coordinator-state.json"
export HOST="127.0.0.1"

info "ACBH Demo Smoke"
info "Building Coordinator..."
"${PNPM[@]}" --filter @acbh/coordinator build || fail "Coordinator build failed"

info "Building Agent..."
AGENT_SUFFIX=""
if [ "$(go env GOOS)" = "windows" ]; then
  AGENT_SUFFIX=".exe"
fi
AGENT="$DEMO_TMPDIR/acbh-agent$AGENT_SUFFIX"
(cd agent && go build -o "$AGENT" .) || fail "Agent build failed"

ready=false
for attempt in 1 2 3; do
  COORDINATOR_PORT=$(choose_port)
  export PORT="$COORDINATOR_PORT"
  : > "$COORD_LOG"
  info "Starting Coordinator on 127.0.0.1:$COORDINATOR_PORT (attempt $attempt/3)..."
  "${PNPM[@]}" --filter @acbh/coordinator start >"$COORD_LOG" 2>&1 &
  COORD_PID=$!
  for _ in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:$COORDINATOR_PORT/health" >/dev/null 2>&1; then
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
  kill "$COORD_PID" 2>/dev/null || true
  wait "$COORD_PID" 2>/dev/null || true
  COORD_PID=""
done
[ "$ready" = true ] || fail "Coordinator failed to start after 3 attempts; check port availability"
COORDINATOR_URL="http://127.0.0.1:$COORDINATOR_PORT"

info "Checking Coordinator health..."
HEALTH=$(curl -sf "$COORDINATOR_URL/health") || fail "Coordinator health check failed"
echo "$HEALTH" | grep -q '"ok":true' || fail "Coordinator not healthy"
info "Coordinator is healthy"

info "Creating group..."
GROUP=$(json_request POST "$COORDINATOR_URL/v1/groups" '{"name":"Demo Group","ownerName":"Demo Owner"}')
GROUP_ID=$(printf '%s' "$GROUP" | json_field groupId)
ACCESS_KEY=$(printf '%s' "$GROUP" | json_field accessKey)
info "Group created: $GROUP_ID"

info "Registering host..."
MEMBER=$(json_request POST "$COORDINATOR_URL/v1/groups/$GROUP_ID/join" \
  "{\"accessKey\":\"$ACCESS_KEY\",\"displayName\":\"Demo Host\"}")
MEMBER_ID=$(printf '%s' "$MEMBER" | json_field memberId)
HOST_RESPONSE=$(json_request POST "$COORDINATOR_URL/v1/hosts/register" \
  "{\"groupId\":\"$GROUP_ID\",\"accessKey\":\"$ACCESS_KEY\",\"memberId\":\"$MEMBER_ID\",\"deviceName\":\"demo-device\",\"platform\":\"$(go env GOOS)\",\"agentVersion\":\"0.1.0\"}")
HOST_ID=$(printf '%s' "$HOST_RESPONSE" | json_field hostId)
HOST_TOKEN=$(printf '%s' "$HOST_RESPONSE" | json_field hostToken)
info "Host registered: $HOST_ID"

if [ "$(go env GOOS)" = "windows" ]; then
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
  "platform": "$(go env GOOS)",
  "agentVersion": "0.1.0"
}
EOF

info "Sending heartbeat..."
HEARTBEAT=$(json_request POST "$COORDINATOR_URL/v1/hosts/heartbeat" \
  "{\"groupId\":\"$GROUP_ID\",\"hostId\":\"$HOST_ID\",\"hostToken\":\"$HOST_TOKEN\",\"status\":\"standby\"}")
echo "$HEARTBEAT" | grep -q '"ok":true' || fail "Heartbeat failed"
info "Heartbeat OK"

info "Creating fake server directory and scanning..."
FAKE_SERVER="$DEMO_TMPDIR/fake-server"
mkdir -p "$FAKE_SERVER/world/region"
echo "world-data" > "$FAKE_SERVER/world/region/r.0.0.mca"
MANIFEST_PATH="$DEMO_TMPDIR/manifest.json"
"$AGENT" scan \
  --server-dir "$FAKE_SERVER" \
  --artifact-kind world-snapshot \
  --artifact-id snap_demo_001 \
  --server-pack-version pack_demo_001 \
  --group-id "$GROUP_ID" \
  --creator-host-id "$HOST_ID" \
  --output "$MANIFEST_PATH" || fail "Scan failed"

info "Validating manifest..."
"$AGENT" manifest validate --file "$MANIFEST_PATH" || fail "Manifest validation failed"
info "Manifest is valid"

info "Pushing artifact to Coordinator..."
"$AGENT" push --manifest "$MANIFEST_PATH" --server-dir "$FAKE_SERVER" || fail "Push failed"
info "Push complete"

info "Checking latest artifact..."
LATEST=$(curl_auth GET "$COORDINATOR_URL/v1/groups/$GROUP_ID/artifacts/latest?artifactKind=world-snapshot" "$HOST_ID" "$HOST_TOKEN" "" "")
echo "$LATEST" | grep -q '"artifactId":"snap_demo_001"' || fail "Latest artifact not found"
info "Latest artifact confirmed"

info "Pulling artifact to restore directory..."
RESTORE_DIR="$DEMO_TMPDIR/restore"
"$AGENT" pull \
  --artifact-kind world-snapshot \
  --artifact-id snap_demo_001 \
  --output-dir "$RESTORE_DIR" || fail "Pull failed"
[ -f "$RESTORE_DIR/world/region/r.0.0.mca" ] || fail "Restored file not found"
info "Restored file confirmed"

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

kill "$COORD_PID" 2>/dev/null || true
wait "$COORD_PID" 2>/dev/null || true
COORD_PID=""

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ACBH Demo Smoke: ALL CHECKS PASSED${NC}"
echo -e "${GREEN}========================================${NC}"
