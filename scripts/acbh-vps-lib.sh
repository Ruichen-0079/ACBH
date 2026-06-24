#!/usr/bin/env bash
# Shared helpers for ACBH Coordinator VPS install, upgrade, rollback, and status.
set -euo pipefail

ACBH_INSTALL_DIR="${ACBH_INSTALL_DIR:-/opt/acbh}"
ACBH_RELEASE_RETENTION="${ACBH_RELEASE_RETENTION:-5}"
ACBH_COORDINATOR_PORT="${ACBH_COORDINATOR_PORT:-6121}"
ACBH_RELAY_PORT="${ACBH_RELAY_PORT:-25565}"
ACBH_MIN_NODE_MAJOR="${ACBH_MIN_NODE_MAJOR:-20}"
ACBH_DISK_MIN_MB="${ACBH_DISK_MIN_MB:-256}"
ACBH_SYSTEMD_SERVICE="${ACBH_SYSTEMD_SERVICE:-acbh-coordinator}"
ACBH_SYSTEMD_UNIT_PATH="${ACBH_SYSTEMD_UNIT_PATH:-/etc/systemd/system/${ACBH_SYSTEMD_SERVICE}.service}"
ACBH_SYSTEMCTL_CMD="${ACBH_SYSTEMCTL_CMD:-systemctl}"
ACBH_CURL_CMD="${ACBH_CURL_CMD:-curl}"
ACBH_SS_CMD="${ACBH_SS_CMD:-ss}"
ACBH_SKIP_ROOT="${ACBH_SKIP_ROOT:-0}"

# Set by acbh-vps-upgrade.sh on failure; consumed by cleanup trap and upgrade reports.
# shellcheck disable=SC2034
ACBH_VPS_UPGRADE_REPORT_ERROR=""
ACBH_VPS_UPGRADE_DRY_RUN="false"
ACBH_VPS_UPGRADE_LOCK_FD=""

acbh_vps_log() {
  printf '[acbh-vps] %s\n' "$*" >&2
}

acbh_vps_die() {
  acbh_vps_log "error: $*"
  exit 1
}

acbh_vps_require_root() {
  if [ "$ACBH_SKIP_ROOT" = "1" ]; then
    return 0
  fi
  if [ "$(id -u)" -ne 0 ]; then
    acbh_vps_die "please run with sudo/root"
  fi
}

acbh_vps_now_utc() {
  date -u +%Y%m%dT%H%M%SZ
}

acbh_vps_now_iso() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

acbh_vps_validate_version_string() {
  local version="$1"
  if [ -z "$version" ]; then
    acbh_vps_die "VERSION must not be empty"
  fi
  if [[ "$version" == *$'\n'* ]] || [[ "$version" == *$'\r'* ]]; then
    acbh_vps_die "VERSION must be a single line"
  fi
  if [[ "$version" == *"/"* ]] || [[ "$version" == *"\\"* ]]; then
    acbh_vps_die "VERSION must not contain path separators: $version"
  fi
  if [[ "$version" == *".."* ]]; then
    acbh_vps_die "VERSION must not contain '..': $version"
  fi
  if [[ "$version" =~ [[:space:]] ]]; then
    acbh_vps_die "VERSION must not contain whitespace: $version"
  fi
  if [[ ! "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    acbh_vps_die "VERSION has invalid characters: $version"
  fi
}

acbh_vps_normalize_version() {
  local version="$1"
  version="${version#v}"
  version="${version#V}"
  printf '%s' "$version"
}

acbh_vps_releases_dir() {
  printf '%s/releases' "$ACBH_INSTALL_DIR"
}

acbh_vps_current_link() {
  printf '%s/current' "$ACBH_INSTALL_DIR"
}

acbh_vps_upgrade_state_path() {
  printf '%s/data/upgrade-state.json' "$ACBH_INSTALL_DIR"
}

acbh_vps_upgrade_logs_dir() {
  printf '%s/logs/upgrades' "$ACBH_INSTALL_DIR"
}

acbh_vps_upgrade_lock_path() {
  printf '%s/.upgrade.lock' "$ACBH_INSTALL_DIR"
}

acbh_vps_ensure_layout() {
  local releases_dir
  releases_dir="$(acbh_vps_releases_dir)"
  mkdir -p \
    "$ACBH_INSTALL_DIR/storage" \
    "$ACBH_INSTALL_DIR/data" \
    "$ACBH_INSTALL_DIR/logs" \
    "$ACBH_INSTALL_DIR/packages" \
    "$releases_dir" \
    "$(acbh_vps_upgrade_logs_dir)"
}

acbh_vps_release_version_path() {
  local release_dir="$1"
  if [ -f "$release_dir/VERSION" ]; then
    tr -d '\r\n' < "$release_dir/VERSION"
    return 0
  fi
  if [ -f "$release_dir/package.json" ] && command -v node >/dev/null 2>&1; then
    node -e "const fs=require('fs'); const p=JSON.parse(fs.readFileSync(process.argv[1],'utf8')); process.stdout.write(String(p.version||''));" "$release_dir/package.json"
    return 0
  fi
  basename "$release_dir"
}

acbh_vps_read_bundle_version() {
  local bundle_dir="$1"
  local version_file="$bundle_dir/VERSION"
  local version=""

  if [ ! -f "$version_file" ]; then
    acbh_vps_die "bundle is missing VERSION"
  fi
  version="$(tr -d '\r\n' < "$version_file")"
  acbh_vps_validate_version_string "$version"
  printf '%s' "$version"
}

acbh_vps_resolve_path() {
  local target="$1"
  if [ ! -e "$target" ] && [ ! -L "$target" ]; then
    acbh_vps_die "path does not exist: $target"
  fi
  (
    cd "$(dirname "$target")" >/dev/null 2>&1
    printf '%s/%s\n' "$(pwd -P)" "$(basename "$target")"
  )
}

acbh_vps_resolve_symlink() {
  local link_path="$1"
  local link_target install_dir

  if [ ! -L "$link_path" ]; then
    return 1
  fi
  link_target="$(readlink "$link_path")"
  install_dir="$(cd "$(dirname "$link_path")" && pwd -P)"
  case "$link_target" in
    /*)
      printf '%s\n' "$link_target"
      ;;
    *)
      printf '%s/%s\n' "$install_dir" "$link_target"
      ;;
  esac
}

acbh_vps_current_release_dir() {
  local current_link resolved version release_dir
  current_link="$(acbh_vps_current_link)"
  if [ -L "$current_link" ]; then
    acbh_vps_resolve_symlink "$current_link"
    return 0
  fi
  if [ -d "$current_link" ] && [ -f "$current_link/VERSION" ]; then
    version="$(tr -d '\r\n' < "$current_link/VERSION")"
    if [[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
      release_dir="$(acbh_vps_release_dir_for_version "$version")"
      if [ -d "$release_dir" ]; then
        printf '%s\n' "$release_dir"
        return 0
      fi
    fi
  fi
  if [ -d "$current_link" ]; then
    resolved="$(acbh_vps_resolve_path "$current_link")"
    printf '%s\n' "$resolved"
    return 0
  fi
  return 1
}

acbh_vps_current_release_name() {
  local current_dir
  if ! current_dir="$(acbh_vps_current_release_dir)"; then
    return 1
  fi
  basename "$current_dir"
}

acbh_vps_write_systemd_unit() {
  local install_dir="$ACBH_INSTALL_DIR"
  acbh_vps_require_root
  mkdir -p "$(dirname "$ACBH_SYSTEMD_UNIT_PATH")"
  cat > "$ACBH_SYSTEMD_UNIT_PATH" <<EOF
[Unit]
Description=ACBH Coordinator
After=network-online.target
Wants=network-online.target

[Service]
User=acbh
Group=acbh
WorkingDirectory=$install_dir/current
EnvironmentFile=$install_dir/.env
ExecStart=/usr/bin/node $install_dir/current/dist/index.js
Restart=always
RestartSec=3
StandardOutput=append:$install_dir/logs/coordinator.log
StandardError=append:$install_dir/logs/coordinator.log

[Install]
WantedBy=multi-user.target
EOF
  "$ACBH_SYSTEMCTL_CMD" daemon-reload
}

acbh_vps_migrate_legacy_layout() {
  local legacy_name legacy_dir releases_dir current_link
  local has_legacy="false"

  if [ -d "$ACBH_INSTALL_DIR/dist" ] && [ ! -L "$(acbh_vps_current_link)" ]; then
    has_legacy="true"
  fi
  if [ "$has_legacy" != "true" ]; then
    return 0
  fi

  acbh_vps_log "migrating legacy flat layout to releases/"
  releases_dir="$(acbh_vps_releases_dir)"
  mkdir -p "$releases_dir"
  legacy_name="legacy-$(acbh_vps_now_utc)"
  legacy_dir="$releases_dir/$legacy_name"
  mkdir -p "$legacy_dir"

  for item in dist package.json node_modules scripts; do
    if [ -e "$ACBH_INSTALL_DIR/$item" ]; then
      mv "$ACBH_INSTALL_DIR/$item" "$legacy_dir/"
    fi
  done

  printf '%s\n' "$legacy_name" > "$legacy_dir/VERSION"

  current_link="$(acbh_vps_current_link)"
  acbh_vps_switch_current_symlink "$legacy_dir"
  acbh_vps_write_systemd_unit
  acbh_vps_log "legacy release saved as releases/$legacy_name"
}

acbh_vps_validate_bundle_structure() {
  local bundle_dir="$1"
  if [ ! -d "$bundle_dir/dist" ]; then
    acbh_vps_die "bundle must contain dist/"
  fi
  if [ ! -f "$bundle_dir/dist/index.js" ]; then
    acbh_vps_die "bundle must contain dist/index.js"
  fi
  if [ ! -f "$bundle_dir/package.json" ]; then
    acbh_vps_die "bundle must contain package.json"
  fi
  if [ ! -f "$bundle_dir/VERSION" ]; then
    acbh_vps_die "bundle must contain VERSION"
  fi
  if [ ! -f "$bundle_dir/SHA256SUMS" ]; then
    acbh_vps_die "bundle must contain SHA256SUMS"
  fi
}

acbh_vps_validate_bundle_checksums() {
  local bundle_dir="$1"
  local sums_file="$bundle_dir/SHA256SUMS"
  local line rel_path checksum expected

  if [ ! -f "$sums_file" ]; then
    acbh_vps_die "bundle is missing SHA256SUMS"
  fi

  while IFS= read -r line || [ -n "$line" ]; do
    line="${line//$'\r'/}"
    [ -z "$line" ] && continue
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    checksum="${line%% *}"
    rel_path="${line#* }"
    rel_path="${rel_path# }"
    if [ -z "$rel_path" ] || [ "$checksum" = "$line" ]; then
      acbh_vps_die "invalid SHA256SUMS line: $line"
    fi
    if [[ "$rel_path" == /* ]] || [[ "$rel_path" == *".."* ]]; then
      acbh_vps_die "SHA256SUMS contains unsafe path: $rel_path"
    fi
    if [ ! -f "$bundle_dir/$rel_path" ]; then
      acbh_vps_die "SHA256SUMS references missing file: $rel_path"
    fi
    if command -v sha256sum >/dev/null 2>&1; then
      expected="$(sha256sum "$bundle_dir/$rel_path" | awk '{print $1}')"
    else
      expected="$(shasum -a 256 "$bundle_dir/$rel_path" | awk '{print $1}')"
    fi
    if [ "$expected" != "$checksum" ]; then
      acbh_vps_die "checksum mismatch for $rel_path"
    fi
  done < "$sums_file"
}

acbh_vps_validate_node_version() {
  local node_major=""
  if ! command -v node >/dev/null 2>&1; then
    acbh_vps_die "node is required but not installed"
  fi
  node_major="$(node -p 'process.versions.node.split(".")[0]')"
  if [ "$node_major" -lt "$ACBH_MIN_NODE_MAJOR" ]; then
    acbh_vps_die "Node.js ${ACBH_MIN_NODE_MAJOR}+ required; current node is $(node --version)"
  fi
}

acbh_vps_check_disk_space() {
  local target_dir="$1"
  local avail_kb min_kb
  min_kb=$((ACBH_DISK_MIN_MB * 1024))
  avail_kb="$(df -Pk "$target_dir" | awk 'NR==2 {print $4}')"
  if [ -z "$avail_kb" ] || [ "$avail_kb" -lt "$min_kb" ]; then
    acbh_vps_die "insufficient disk space under $target_dir (need ${ACBH_DISK_MIN_MB}MB free)"
  fi
}

acbh_vps_acquire_upgrade_lock() {
  local lock_file
  lock_file="$(acbh_vps_upgrade_lock_path)"
  mkdir -p "$ACBH_INSTALL_DIR"
  exec {ACBH_VPS_UPGRADE_LOCK_FD}>"$lock_file"
  if command -v flock >/dev/null 2>&1; then
    if ! flock -n "$ACBH_VPS_UPGRADE_LOCK_FD"; then
      acbh_vps_die "another upgrade is in progress (lock: $lock_file)"
    fi
  else
    acbh_vps_log "flock not available; upgrade lock is best-effort"
  fi
}

acbh_vps_release_upgrade_lock() {
  if [ -n "$ACBH_VPS_UPGRADE_LOCK_FD" ]; then
    eval "exec ${ACBH_VPS_UPGRADE_LOCK_FD}>&-"
    ACBH_VPS_UPGRADE_LOCK_FD=""
  fi
}

acbh_vps_copy_bundle_to_dir() {
  local bundle_dir="$1"
  local target_dir="$2"
  mkdir -p "$target_dir"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a \
      --exclude '.upgrade-staging' \
      "$bundle_dir/" "$target_dir/"
  else
    cp -a "$bundle_dir/." "$target_dir/"
  fi
}

acbh_vps_validate_release_dir() {
  local release_dir="$1"
  acbh_vps_validate_bundle_structure "$release_dir"
  acbh_vps_validate_bundle_checksums "$release_dir"
}

acbh_vps_backup_coordinator_state() {
  local state_file backup_file ts
  state_file="$ACBH_INSTALL_DIR/data/coordinator-state.json"
  if [ ! -f "$state_file" ]; then
    return 0
  fi
  ts="$(acbh_vps_now_utc)"
  backup_file="$ACBH_INSTALL_DIR/data/coordinator-state.json.bak-$ts"
  cp -a "$state_file" "$backup_file"
  printf '%s' "$backup_file"
}

acbh_vps_restore_coordinator_state() {
  local backup_file="$1"
  local state_file="$ACBH_INSTALL_DIR/data/coordinator-state.json"
  if [ -z "$backup_file" ] || [ ! -f "$backup_file" ]; then
    return 0
  fi
  cp -a "$backup_file" "$state_file"
}

acbh_vps_switch_current_symlink() {
  local release_dir="$1"
  local current_link rel_name target version
  current_link="$(acbh_vps_current_link)"
  if [ -f "$release_dir/VERSION" ]; then
    version="$(tr -d '\r\n' < "$release_dir/VERSION")"
    rel_name="releases/$version"
  else
    rel_name="releases/$(basename "$release_dir")"
  fi
  target="$ACBH_INSTALL_DIR/$rel_name"
  if [ ! -d "$target" ]; then
    acbh_vps_die "release directory not found: $target"
  fi
  rm -rf "$current_link"
  if ln -sfn "$rel_name" "$current_link" 2>/dev/null && [ -f "$current_link/dist/index.js" ]; then
    return 0
  fi
  if command -v cmd.exe >/dev/null 2>&1 && command -v cygpath >/dev/null 2>&1; then
    cmd.exe //c mklink /J "$(cygpath -w "$current_link")" "$(cygpath -w "$target")" >/dev/null 2>&1 || true
    if [ -f "$current_link/dist/index.js" ]; then
      return 0
    fi
  fi
  acbh_vps_die "failed to switch current to $rel_name"
}

acbh_vps_service_stop() {
  "$ACBH_SYSTEMCTL_CMD" stop "$ACBH_SYSTEMD_SERVICE"
}

acbh_vps_service_start() {
  "$ACBH_SYSTEMCTL_CMD" start "$ACBH_SYSTEMD_SERVICE"
}

acbh_vps_service_restart() {
  "$ACBH_SYSTEMCTL_CMD" restart "$ACBH_SYSTEMD_SERVICE"
}

acbh_vps_service_enable() {
  "$ACBH_SYSTEMCTL_CMD" enable "$ACBH_SYSTEMD_SERVICE"
}

acbh_vps_extract_json_field() {
  local json="$1"
  local field="$2"
  node -e 'const j=JSON.parse(process.argv[1]); const f=process.argv[2]; const v=j[f]; process.stdout.write(v===undefined?"":String(v));' "$json" "$field"
}

acbh_vps_check_health() {
  local expected_version="$1"
  local health_url health_json actual_version
  local expected_norm actual_norm

  health_url="http://127.0.0.1:${ACBH_COORDINATOR_PORT}/health"
  if ! health_json="$("$ACBH_CURL_CMD" -fsS "$health_url" 2>/dev/null)"; then
    acbh_vps_log "health check request failed"
    return 1
  fi
  if [ -z "$health_json" ]; then
    acbh_vps_log "health check returned empty response"
    return 1
  fi
  if [ "$(acbh_vps_extract_json_field "$health_json" ok)" != "true" ]; then
    acbh_vps_log "health check returned ok!=true"
    return 1
  fi
  actual_version="$(acbh_vps_extract_json_field "$health_json" version)"
  expected_norm="$(acbh_vps_normalize_version "$expected_version")"
  actual_norm="$(acbh_vps_normalize_version "$actual_version")"
  if [ "$actual_norm" != "$expected_norm" ]; then
    acbh_vps_log "health version mismatch: expected $expected_norm got $actual_norm"
    return 1
  fi
  return 0
}

acbh_vps_assert_health() {
  acbh_vps_check_health "$1" || acbh_vps_die "health check failed for $1"
}

acbh_vps_check_ports() {
  local port
  for port in "$ACBH_COORDINATOR_PORT" "$ACBH_RELAY_PORT"; do
    if ! "$ACBH_SS_CMD" -ltn | grep -q ":${port} "; then
      acbh_vps_log "port $port is not listening"
      return 1
    fi
  done
  return 0
}

acbh_vps_assert_ports() {
  acbh_vps_check_ports || acbh_vps_die "required ports are not listening"
}

acbh_vps_write_upgrade_state() {
  local current="$1"
  local previous="$2"
  local state_path
  state_path="$(acbh_vps_upgrade_state_path)"
  mkdir -p "$(dirname "$state_path")"
  node -e '
const fs=require("fs");
const path=process.argv[1];
const current=process.argv[2];
const previous=process.argv[3];
let prior={};
if (fs.existsSync(path)) {
  try { prior=JSON.parse(fs.readFileSync(path,"utf8")); } catch {}
}
const payload={
  current,
  previous: previous || prior.current || "",
  updatedAt: new Date().toISOString(),
};
fs.writeFileSync(path, JSON.stringify(payload, null, 2) + "\n");
' "$state_path" "$current" "$previous"
}

acbh_vps_read_upgrade_state_previous() {
  local state_path
  state_path="$(acbh_vps_upgrade_state_path)"
  if [ ! -f "$state_path" ]; then
    return 1
  fi
  node -e '
const fs=require("fs");
const path=process.argv[1];
const data=JSON.parse(fs.readFileSync(path,"utf8"));
process.stdout.write(String(data.previous||""));
' "$state_path"
}

acbh_vps_prune_releases() {
  local keep="$ACBH_RELEASE_RETENTION"
  local releases_dir current_target release

  releases_dir="$(acbh_vps_releases_dir)"
  if [ ! -d "$releases_dir" ]; then
    return 0
  fi

  if current_target="$(acbh_vps_current_release_dir 2>/dev/null || true)"; then
    :
  else
    current_target=""
  fi

  while IFS= read -r release; do
    [ -n "$release" ] || continue
    if [ -n "$current_target" ] && [ "$release" = "$current_target" ]; then
      continue
    fi
    acbh_vps_log "pruning old release $(basename "$release")"
    rm -rf "$release"
  done < <(node -e '
const fs=require("fs");
const path=require("path");
const releasesDir=process.argv[1];
const keep=Number(process.argv[2]);
const current=process.argv[3]||"";
let entries=[];
for (const name of fs.readdirSync(releasesDir)) {
  if (name.startsWith(".upgrade-staging")) continue;
  const full=path.join(releasesDir,name);
  if (!fs.statSync(full).isDirectory()) continue;
  entries.push({full,mtime:fs.statSync(full).mtimeMs});
}
entries.sort((a,b)=>b.mtime-a.mtime);
for (const entry of entries.slice(keep)) {
  if (entry.full===current) continue;
  process.stdout.write(entry.full+"\n");
}
' "$releases_dir" "$keep" "$current_target")
}

acbh_vps_json_escape() {
  node -e 'process.stdout.write(JSON.stringify(process.argv[1]??""));' "$1"
}

acbh_vps_write_upgrade_report() {
  local version="$1"
  local previous="$2"
  local status="$3"
  local error_msg="${4:-}"
  local report_dir report_file ts
  local state_backup="${ACBH_VPS_STATE_BACKUP:-}"

  report_dir="$(acbh_vps_upgrade_logs_dir)"
  mkdir -p "$report_dir"
  ts="$(acbh_vps_now_utc)"
  report_file="$report_dir/${ts}-${version}.json"

  node -e '
const fs=require("fs");
const file=process.argv[1];
const payload={
  timestamp: process.argv[2],
  version: process.argv[3],
  previous_version: process.argv[4],
  status: process.argv[5],
  dry_run: process.argv[6]==="true",
  state_backup: process.argv[7] || null,
  error: process.argv[8] || null,
};
fs.writeFileSync(file, JSON.stringify(payload, null, 2) + "\n");
' "$report_file" "$(acbh_vps_now_iso)" "$version" "$previous" "$status" "$ACBH_VPS_UPGRADE_DRY_RUN" "$state_backup" "$error_msg"

  acbh_vps_log "upgrade report: $report_file"
}

acbh_vps_release_dir_for_version() {
  local version="$1"
  printf '%s/%s' "$(acbh_vps_releases_dir)" "$version"
}

acbh_vps_staging_dir_for_version() {
  local version="$1"
  printf '%s/.upgrade-staging-%s' "$(acbh_vps_releases_dir)" "$version"
}

acbh_vps_cleanup_staging_dir() {
  local staging_dir="$1"
  if [ -n "$staging_dir" ] && [ -d "$staging_dir" ]; then
    rm -rf "$staging_dir"
  fi
}

acbh_vps_perform_rollback() {
  local previous_release_dir="$1"
  local expected_version="$2"
  local state_backup="${3:-}"

  acbh_vps_log "rolling back to $(basename "$previous_release_dir")"
  acbh_vps_service_stop || true
  acbh_vps_switch_current_symlink "$previous_release_dir"
  acbh_vps_restore_coordinator_state "$state_backup"
  acbh_vps_service_start
  acbh_vps_assert_health "$expected_version"
  acbh_vps_assert_ports
}