#!/usr/bin/env bash
# Run all project checks in order and stop on the first failure.
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }
section() { echo -e "\n${CYAN}-- $* --${NC}"; }

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

cd "$(dirname "$0")/.."

require_command go
require_command node
resolve_pnpm

section "Go vet"
(cd agent && go vet ./...) || fail "go vet"

section "Go tests"
(cd agent && go test ./... -count=1) || fail "go test"

section "Coordinator build"
"${PNPM[@]}" --filter @acbh/coordinator build || fail "coordinator build"

section "Coordinator tests"
"${PNPM[@]}" --filter @acbh/coordinator test || fail "coordinator test"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ACBH verify-all: ALL CHECKS PASSED${NC}"
echo -e "${GREEN}========================================${NC}"
