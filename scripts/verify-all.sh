#!/usr/bin/env bash
# ACBH verify-all script
# Runs all project checks and tests in order.
# Exits non-zero on first failure.
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }
section() { echo -e "\n${CYAN}── $* ──${NC}"; }

cd "$(dirname "$0")/.."

section "Go vet"
(cd agent && go vet ./...) || fail "go vet"

section "Go tests"
(cd agent && go test ./... -count=1) || fail "go test"

section "Coordinator build"
pnpm build:coordinator || fail "pnpm build:coordinator"

section "Coordinator tests"
pnpm --filter @acbh/coordinator test || fail "coordinator test"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ACBH verify-all: ALL CHECKS PASSED${NC}"
echo -e "${GREEN}========================================${NC}"
