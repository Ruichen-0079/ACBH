#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
AGENT_DIR="$REPO_ROOT/agent"

echo "=== ACBH Relay-Only Demo ==="
echo ""

pnpm --filter @acbh/coordinator build > /dev/null 2>&1

cd "$AGENT_DIR"
go run ./cmd/relay-demo "$@"
