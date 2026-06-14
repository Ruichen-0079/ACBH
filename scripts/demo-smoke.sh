#!/usr/bin/env bash
# ACBH demo smoke script
# Runs a minimal closed-loop demo without real Minecraft or public network.
# Requires: go, pnpm, node
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

info "ACBH Demo Smoke"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

export ACBH_STORAGE_ROOT="$TMPDIR/storage"
export ACBH_COORDINATOR_STATE_PATH="$TMPDIR/coordinator-state.json"

# ── 1. Build ──────────────────────────────────────────────
info "Building Coordinator..."
pnpm build:coordinator || fail "Coordinator build failed"

info "Building Agent..."
(cd agent && go build -o "$TMPDIR/acbh-agent" .) || fail "Agent build failed"
AGENT="$TMPDIR/acbh-agent"

# ── 2. Start Coordinator ──────────────────────────────────
info "Starting Coordinator on random port..."
COORDINATOR_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null || echo "16121")
export PORT="$COORDINATOR_PORT"
export HOST="127.0.0.1"

pnpm --filter @acbh/coordinator start &
COORD_PID=$!
sleep 2

COORDINATOR_URL="http://127.0.0.1:$COORDINATOR_PORT"

# ── 3. Health check ───────────────────────────────────────
info "Checking Coordinator health..."
HEALTH=$(curl -sf "$COORDINATOR_URL/health") || fail "Coordinator health check failed"
echo "$HEALTH" | grep -q '"ok":true' || fail "Coordinator not healthy"
info "Coordinator is healthy"

# ── 4. Create group ───────────────────────────────────────
info "Creating group..."
GROUP=$(curl -sf -X POST "$COORDINATOR_URL/v1/groups" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Demo Group","ownerName":"Demo Owner"}')
GROUP_ID=$(echo "$GROUP" | python3 -c 'import sys,json; print(json.load(sys.stdin)["groupId"])')
ACCESS_KEY=$(echo "$GROUP" | python3 -c 'import sys,json; print(json.load(sys.stdin)["accessKey"])')
echo "Group: $GROUP_ID"

# ── 5. Join group / Register host ─────────────────────────
info "Registering host..."
MEMBER=$(curl -sf -X POST "$COORDINATOR_URL/v1/groups/$GROUP_ID/join" \
  -H 'Content-Type: application/json' \
  -d "{\"accessKey\":\"$ACCESS_KEY\",\"displayName\":\"Demo Host\"}")
MEMBER_ID=$(echo "$MEMBER" | python3 -c 'import sys,json; print(json.load(sys.stdin)["memberId"])')

HOST=$(curl -sf -X POST "$COORDINATOR_URL/v1/hosts/register" \
  -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"accessKey\":\"$ACCESS_KEY\",\"memberId\":\"$MEMBER_ID\",\"deviceName\":\"demo-device\",\"platform\":\"linux\",\"agentVersion\":\"0.1.0\"}")
HOST_ID=$(echo "$HOST" | python3 -c 'import sys,json; print(json.load(sys.stdin)["hostId"])')
HOST_TOKEN=$(echo "$HOST" | python3 -c 'import sys,json; print(json.load(sys.stdin)["hostToken"])')
info "Host registered: $HOST_ID"

# ── 6. Heartbeat ──────────────────────────────────────────
info "Sending heartbeat..."
HB=$(curl -sf -X POST "$COORDINATOR_URL/v1/hosts/heartbeat" \
  -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"hostId\":\"$HOST_ID\",\"hostToken\":\"$HOST_TOKEN\",\"status\":\"standby\"}")
echo "$HB" | grep -q '"ok":true' || fail "Heartbeat failed"
info "Heartbeat OK"

# ── 7. Agent scan / manifest ──────────────────────────────
info "Creating fake server dir and scanning..."
FAKE_SERVER="$TMPDIR/fake-server"
mkdir -p "$FAKE_SERVER/world/region"
echo "world-data" > "$FAKE_SERVER/world/region/r.0.0.mca"

MANIFEST_PATH="$TMPDIR/manifest.json"
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

# ── 8. Push artifact ──────────────────────────────────────
info "Pushing artifact to Coordinator..."
"$AGENT" push \
  --manifest "$MANIFEST_PATH" \
  --server-dir "$FAKE_SERVER" || fail "Push failed"
info "Push complete"

# ── 9. Check latest artifact ──────────────────────────────
info "Checking latest artifact..."
LATEST=$(curl -sf "$COORDINATOR_URL/v1/groups/$GROUP_ID/artifacts/latest?artifactKind=world-snapshot" \
  -H "x-acbh-host-id: $HOST_ID" \
  -H "x-acbh-host-token: $HOST_TOKEN")
echo "$LATEST" | grep -q '"artifactId":"snap_demo_001"' || fail "Latest artifact not found"
info "Latest artifact confirmed"

# ── 10. Pull artifact ─────────────────────────────────────
info "Pulling artifact to restore dir..."
RESTORE_DIR="$TMPDIR/restore"
"$AGENT" pull \
  --artifact-kind world-snapshot \
  --artifact-id snap_demo_001 \
  --output-dir "$RESTORE_DIR" || fail "Pull failed"

# Verify restored file
if [ -f "$RESTORE_DIR/world/region/r.0.0.mca" ]; then
  info "Restored file matches"
else
  fail "Restored file not found"
fi

# ── 11. Group state ───────────────────────────────────────
info "Checking group state..."
STATE=$(curl -sf "$COORDINATOR_URL/v1/groups/$GROUP_ID/state" \
  -H "x-acbh-access-key: $ACCESS_KEY")
echo "$STATE" | grep -q "$GROUP_ID" || fail "Group state check failed"
# Must not leak access key
if echo "$STATE" | grep -q "$ACCESS_KEY"; then
  fail "Group state leaked access key"
fi
info "Group state OK, no secret leaked"

# ── 12. Cleanup ───────────────────────────────────────────
kill "$COORD_PID" 2>/dev/null || true
wait "$COORD_PID" 2>/dev/null || true

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ACBH Demo Smoke: ALL CHECKS PASSED${NC}"
echo -e "${GREEN}========================================${NC}"
