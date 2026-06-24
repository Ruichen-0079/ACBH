#!/usr/bin/env bash
# Integration tests for ACBH VPS upgrade scripts (no root required).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_SCRIPTS="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=../acbh-vps-lib.sh
. "$REPO_SCRIPTS/acbh-vps-lib.sh"

TESTS_RUN=0
TESTS_FAILED=0
TEST_ROOT=""
MOCK_BIN=""

fail_test() {
  echo "FAIL: $*" >&2
  TESTS_FAILED=$((TESTS_FAILED + 1))
}

pass_test() {
  echo "PASS: $*"
  TESTS_RUN=$((TESTS_RUN + 1))
}

finish_test() {
  local name="$1"
  local failures_before="$2"
  if [ "$TESTS_FAILED" -eq "$failures_before" ]; then
    pass_test "$name"
  fi
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local message="${3:-}"
  if [ "$expected" != "$actual" ]; then
    fail_test "${message:-assert_eq}: expected '$expected' got '$actual'"
  fi
}

assert_file_exists() {
  local path="$1"
  local message="${2:-}"
  if [ ! -e "$path" ]; then
    fail_test "${message:-missing file}: $path"
  fi
}

assert_current_release() {
  local version="$1"
  local expected_dir link_target
  expected_dir="$ACBH_INSTALL_DIR/releases/$version"
  assert_file_exists "$expected_dir/dist/index.js"
  assert_file_exists "$ACBH_INSTALL_DIR/current/dist/index.js"
  assert_eq "$(tr -d '\r\n' < "$expected_dir/VERSION")" "$(tr -d '\r\n' < "$ACBH_INSTALL_DIR/current/VERSION")"
  link_target="$(readlink "$ACBH_INSTALL_DIR/current" 2>/dev/null || true)"
  if [ -n "$link_target" ]; then
    assert_eq "releases/$version" "$link_target" "current symlink"
  fi
}

write_mock_bin() {
  mkdir -p "$MOCK_BIN"

  cat > "$MOCK_BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
STATE_FILE="${ACBH_MOCK_STATE_FILE:?}"
cmd="${1:-}"
svc="${2:-}"
log="$STATE_FILE/systemctl.log"
mkdir -p "$(dirname "$STATE_FILE")"
printf '%s\n' "$*" >> "$log"
case "$cmd" in
  daemon-reload|enable) exit 0 ;;
  stop)
    printf 'stopped\n' > "$STATE_FILE/service.state"
    exit 0
    ;;
  start|restart)
    printf 'started\n' > "$STATE_FILE/service.state"
    exit 0
    ;;
  status)
    cat "$STATE_FILE/service.state" 2>/dev/null || printf 'unknown\n'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
EOF

  cat > "$MOCK_BIN/ss" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
STATE_FILE="${ACBH_MOCK_STATE_FILE:?}"
if [ -f "$STATE_FILE/ports.down" ]; then
  exit 0
fi
printf 'LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n' "${ACBH_COORDINATOR_PORT:-6121}"
printf 'LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n' "${ACBH_RELAY_PORT:-25565}"
EOF

  cat > "$MOCK_BIN/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
STATE_FILE="${ACBH_MOCK_STATE_FILE:?}"
url="${*: -1}"
if [ -f "$STATE_FILE/health.fail" ]; then
  count="$(cat "$STATE_FILE/health.fail.count" 2>/dev/null || echo 0)"
  if [ "$count" -lt 1 ]; then
    printf '1\n' > "$STATE_FILE/health.fail.count"
    exit 22
  fi
fi
version="$(cat "$STATE_FILE/health.version" 2>/dev/null || echo "0.0.0")"
if [[ "$url" == *"/health" ]]; then
  printf '{"ok":true,"service":"acbh-coordinator","version":"%s"}\n' "$version"
  exit 0
fi
if [[ "$url" == *"/v1/bootstrap/manifest" ]]; then
  printf '{"version":1,"packages":[]}\n'
  exit 0
fi
printf '{}'
EOF

  chmod +x "$MOCK_BIN/systemctl" "$MOCK_BIN/ss" "$MOCK_BIN/curl"
}

setup_test_env() {
  TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/acbh-vps-test.XXXXXX")"
  MOCK_BIN="$TEST_ROOT/mock-bin"
  write_mock_bin

  export ACBH_INSTALL_DIR="$TEST_ROOT/opt/acbh"
  export ACBH_SKIP_ROOT=1
  export ACBH_SYSTEMD_UNIT_PATH="$TEST_ROOT/etc/systemd/system/acbh-coordinator.service"
  export ACBH_SYSTEMCTL_CMD="systemctl"
  export ACBH_CURL_CMD="curl"
  export ACBH_SS_CMD="ss"
  export ACBH_MOCK_STATE_FILE="$TEST_ROOT/mock-state"
  export ACBH_RELEASE_RETENTION=2
  export PATH="$MOCK_BIN:$PATH"
  mkdir -p "$ACBH_MOCK_STATE_FILE"
  printf 'stopped\n' > "$ACBH_MOCK_STATE_FILE/service.state"
  printf '0.3.5-hotfix1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
}

teardown_test_env() {
  if [ -n "$TEST_ROOT" ] && [ -d "$TEST_ROOT" ]; then
    rm -rf "$TEST_ROOT"
  fi
}

bundle_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  else
    shasum -a 256 "$file" | awk '{print $1}'
  fi
}

write_bundle() {
  local bundle_dir="$1"
  local version="$2"
  local health_version="${3:-$2}"

  mkdir -p "$bundle_dir/dist"
  printf 'module.exports = {};\n' > "$bundle_dir/dist/index.js"
  cat > "$bundle_dir/package.json" <<EOF
{"name":"@acbh/coordinator","version":"$(echo "$health_version" | sed 's/^v//')","private":true}
EOF
  printf '%s\n' "$version" > "$bundle_dir/VERSION"
  {
    printf '%s  dist/index.js\n' "$(bundle_sha256 "$bundle_dir/dist/index.js")"
    printf '%s  package.json\n' "$(bundle_sha256 "$bundle_dir/package.json")"
    printf '%s  VERSION\n' "$(bundle_sha256 "$bundle_dir/VERSION")"
  } > "$bundle_dir/SHA256SUMS"
}

run_install() {
  bash "$REPO_SCRIPTS/install-acbh-vps.sh" --public-host test.example.com --install-dir "$ACBH_INSTALL_DIR"
}

run_upgrade() {
  local bundle="$1"
  shift
  bash "$REPO_SCRIPTS/acbh-vps-upgrade.sh" --install-dir "$ACBH_INSTALL_DIR" "$bundle" "$@"
}

run_rollback() {
  bash "$REPO_SCRIPTS/acbh-vps-rollback.sh" --install-dir "$ACBH_INSTALL_DIR" "$@"
}

test_new_layout_install() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  assert_file_exists "$ACBH_INSTALL_DIR/releases"
  assert_file_exists "$ACBH_INSTALL_DIR/.env"
  assert_file_exists "$ACBH_SYSTEMD_UNIT_PATH" "systemd unit generated"
  grep -q 'WorkingDirectory=.*/current' "$ACBH_SYSTEMD_UNIT_PATH" || fail_test "systemd WorkingDirectory must use current"
  finish_test "new layout install" "$failures_before"
  teardown_test_env
}

test_legacy_migration() {
  local failures_before=$TESTS_FAILED
  local legacy_name
  setup_test_env
  mkdir -p "$ACBH_INSTALL_DIR"/{dist,storage,data,logs,packages}
  printf 'legacy\n' > "$ACBH_INSTALL_DIR/dist/index.js"
  printf '{"version":"0.2.0"}\n' > "$ACBH_INSTALL_DIR/package.json"
  run_install
  assert_file_exists "$ACBH_INSTALL_DIR/current"
  legacy_name="$(basename "$(acbh_vps_current_release_dir)")"
  [[ "$legacy_name" == legacy-* ]] || fail_test "legacy release should be named legacy-*"
  assert_file_exists "$(acbh_vps_current_release_dir)/dist/index.js"
  [ ! -d "$ACBH_INSTALL_DIR/dist" ] || fail_test "legacy dist should be moved out of install root"
  finish_test "legacy migration" "$failures_before"
  teardown_test_env
}

test_env_preserved() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  mkdir -p "$ACBH_INSTALL_DIR"
  printf 'CUSTOM=value\n' > "$ACBH_INSTALL_DIR/.env"
  run_install
  assert_eq "CUSTOM=value" "$(tr -d '\r\n' < "$ACBH_INSTALL_DIR/.env")" ".env preserved on install"
  finish_test ".env preserved" "$failures_before"
  teardown_test_env
}

test_persistent_dirs_preserved() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  mkdir -p "$ACBH_INSTALL_DIR"/{storage,data,logs,packages}
  printf 'blob\n' > "$ACBH_INSTALL_DIR/storage/keep.txt"
  printf 'state\n' > "$ACBH_INSTALL_DIR/data/keep.txt"
  printf 'log\n' > "$ACBH_INSTALL_DIR/logs/keep.txt"
  printf 'pkg\n' > "$ACBH_INSTALL_DIR/packages/keep.txt"
  run_install
  local bundle="$TEST_ROOT/bundle-v0.4.0-alpha1"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0.4.0-alpha1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle"
  assert_file_exists "$ACBH_INSTALL_DIR/storage/keep.txt"
  assert_file_exists "$ACBH_INSTALL_DIR/data/keep.txt"
  assert_file_exists "$ACBH_INSTALL_DIR/logs/keep.txt"
  assert_file_exists "$ACBH_INSTALL_DIR/packages/keep.txt"
  finish_test "storage/data/logs/packages preserved" "$failures_before"
  teardown_test_env
}

test_current_symlink_switch() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  local bundle_a="$TEST_ROOT/bundle-a"
  local bundle_b="$TEST_ROOT/bundle-b"
  write_bundle "$bundle_a" "v0.4.0-alpha1" "0.4.0-alpha1"
  write_bundle "$bundle_b" "v0.4.0-alpha2" "0.4.0-alpha2"
  printf '0.4.0-alpha1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle_a"
  assert_current_release "v0.4.0-alpha1"
  printf '0.4.0-alpha2\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle_b"
  assert_current_release "v0.4.0-alpha2"
  finish_test "current symlink switch" "$failures_before"
  teardown_test_env
}

test_checksum_failure() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  local bundle="$TEST_ROOT/bad-bundle"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0000000000000000000000000000000000000000000000000000000000000000  dist/index.js\n' >> "$bundle/SHA256SUMS"
  if run_upgrade "$bundle" 2>/dev/null; then
    fail_test "checksum failure should abort upgrade"
  fi
  finish_test "checksum failure" "$failures_before"
  teardown_test_env
}

test_health_failure_rollback() {
  local failures_before=$TESTS_FAILED
  local report=""
  setup_test_env
  run_install
  local bundle_old="$TEST_ROOT/bundle-old"
  local bundle_new="$TEST_ROOT/bundle-new"
  write_bundle "$bundle_old" "v0.3.5-hotfix1" "0.3.5-hotfix1"
  write_bundle "$bundle_new" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0.3.5-hotfix1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle_old"
  touch "$ACBH_MOCK_STATE_FILE/health.fail"
  if run_upgrade "$bundle_new" 2>/dev/null; then
    fail_test "health failure should abort upgrade"
  else
    assert_current_release "v0.3.5-hotfix1"
    report="$(find "$ACBH_INSTALL_DIR/logs/upgrades" -name '*-v0.4.0-alpha1.json' 2>/dev/null | head -n 1)"
    assert_file_exists "$report" "rollback report"
    grep -q '"status": "rollback_success"' "$report" || fail_test "report should contain rollback_success"
  fi
  finish_test "health failure rollback" "$failures_before"
  teardown_test_env
}

test_version_mismatch_rollback() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  local bundle_old="$TEST_ROOT/bundle-old"
  local bundle_new="$TEST_ROOT/bundle-new"
  write_bundle "$bundle_old" "v0.3.5-hotfix1" "0.3.5-hotfix1"
  write_bundle "$bundle_new" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0.3.5-hotfix1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle_old"
  printf '9.9.9\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  if run_upgrade "$bundle_new" 2>/dev/null; then
    fail_test "version mismatch should abort upgrade"
  else
    assert_current_release "v0.3.5-hotfix1"
  fi
  finish_test "version mismatch rollback" "$failures_before"
  teardown_test_env
}

test_dry_run() {
  local failures_before=$TESTS_FAILED
  local report=""
  setup_test_env
  run_install
  local bundle="$TEST_ROOT/bundle"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  run_upgrade "$bundle" --dry-run
  [ ! -e "$ACBH_INSTALL_DIR/releases/v0.4.0-alpha1" ] || fail_test "dry-run must not create release"
  report="$(find "$ACBH_INSTALL_DIR/logs/upgrades" -name '*-v0.4.0-alpha1.json' 2>/dev/null | head -n 1)"
  assert_file_exists "$report" "dry-run report"
  grep -q '"dry_run": true' "$report" || fail_test "dry-run report flag missing"
  finish_test "dry-run" "$failures_before"
  teardown_test_env
}

test_upgrade_lock() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  local bundle="$TEST_ROOT/bundle"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0.4.0-alpha1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  exec 8> "$ACBH_INSTALL_DIR/.upgrade.lock"
  if ! command -v flock >/dev/null 2>&1; then
    fail_test "flock is required for upgrade lock test"
    finish_test "upgrade lock" "$failures_before"
    teardown_test_env
    return
  fi
  flock -n 8 || fail_test "unable to acquire test lock"
  if run_upgrade "$bundle" 2>/dev/null; then
    fail_test "upgrade should fail when lock is held"
  fi
  flock -u 8
  finish_test "upgrade lock" "$failures_before"
  teardown_test_env
}

test_release_retention() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  export ACBH_RELEASE_RETENTION=2
  run_install
  local versions=("v0.4.0-alpha1" "v0.4.0-alpha2" "v0.4.0-alpha3")
  local bundle
  for version in "${versions[@]}"; do
    bundle="$TEST_ROOT/bundle-$version"
    write_bundle "$bundle" "$version" "${version#v}"
    printf '%s\n' "${version#v}" > "$ACBH_MOCK_STATE_FILE/health.version"
    run_upgrade "$bundle"
    touch "$ACBH_INSTALL_DIR/releases/$version/package.json"
    sleep 1
  done
  assert_file_exists "$ACBH_INSTALL_DIR/releases/v0.4.0-alpha3"
  assert_file_exists "$ACBH_INSTALL_DIR/releases/v0.4.0-alpha2"
  [ ! -d "$ACBH_INSTALL_DIR/releases/v0.4.0-alpha1" ] || fail_test "oldest release should be pruned"
  finish_test "release retention" "$failures_before"
  teardown_test_env
}

test_systemd_unit_generation() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  grep -q "ExecStart=/usr/bin/node $ACBH_INSTALL_DIR/current/dist/index.js" "$ACBH_SYSTEMD_UNIT_PATH" \
    || fail_test "systemd ExecStart must target current/dist/index.js"
  finish_test "systemd unit generation" "$failures_before"
  teardown_test_env
}

test_idempotent_upgrade() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  local bundle="$TEST_ROOT/bundle"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0.4.0-alpha1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle"
  run_upgrade "$bundle"
  assert_current_release "v0.4.0-alpha1"
  finish_test "idempotent upgrade" "$failures_before"
  teardown_test_env
}

test_paths_with_spaces() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  local spaced_root="$TEST_ROOT/with spaces"
  export ACBH_INSTALL_DIR="$spaced_root/opt/acbh"
  export ACBH_SYSTEMD_UNIT_PATH="$spaced_root/etc/systemd/system/acbh-coordinator.service"
  mkdir -p "$(dirname "$ACBH_SYSTEMD_UNIT_PATH")"
  run_install
  local bundle="$spaced_root/my bundle"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '0.4.0-alpha1\n' > "$ACBH_MOCK_STATE_FILE/health.version"
  run_upgrade "$bundle"
  assert_current_release "v0.4.0-alpha1"
  finish_test "paths with spaces" "$failures_before"
  teardown_test_env
}

test_malicious_version_rejection() {
  local failures_before=$TESTS_FAILED
  setup_test_env
  run_install
  local bundle="$TEST_ROOT/evil-bundle"
  write_bundle "$bundle" "v0.4.0-alpha1" "0.4.0-alpha1"
  printf '../escape\n' > "$bundle/VERSION"
  if run_upgrade "$bundle" 2>/dev/null; then
    fail_test "malicious VERSION should be rejected"
  fi
  finish_test "malicious path rejection in bundle VERSION" "$failures_before"
  teardown_test_env
}

run_test() {
  set +e
  "$@"
  local rc=$?
  set -e
  return "$rc"
}

main() {
  if ! command -v node >/dev/null 2>&1; then
    echo "node is required to run VPS upgrade tests" >&2
    exit 1
  fi

  run_test test_new_layout_install
  run_test test_legacy_migration
  run_test test_env_preserved
  run_test test_persistent_dirs_preserved
  run_test test_current_symlink_switch
  run_test test_checksum_failure
  run_test test_health_failure_rollback
  run_test test_version_mismatch_rollback
  run_test test_dry_run
  run_test test_upgrade_lock
  run_test test_release_retention
  run_test test_systemd_unit_generation
  run_test test_idempotent_upgrade
  run_test test_paths_with_spaces
  run_test test_malicious_version_rejection

  echo
  echo "Tests run: $TESTS_RUN"
  echo "Tests failed: $TESTS_FAILED"
  if [ "$TESTS_FAILED" -ne 0 ]; then
    exit 1
  fi
}

main "$@"