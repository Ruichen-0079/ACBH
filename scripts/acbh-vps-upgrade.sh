#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./acbh-vps-lib.sh
. "$SCRIPT_DIR/acbh-vps-lib.sh"

BUNDLE_DIR=""
DRY_RUN="false"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir) ACBH_INSTALL_DIR="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help)
      cat <<'EOF'
usage: sudo bash acbh-vps-upgrade.sh /path/to/bundle [--dry-run]
EOF
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      acbh_vps_die "unknown argument: $1"
      ;;
    *)
      if [ -z "$BUNDLE_DIR" ]; then
        BUNDLE_DIR="$1"
      else
        acbh_vps_die "unexpected argument: $1"
      fi
      shift
      ;;
  esac
done

acbh_vps_require_root

if [ -z "$BUNDLE_DIR" ]; then
  acbh_vps_die "usage: sudo bash acbh-vps-upgrade.sh /path/to/bundle [--dry-run]"
fi
if [ ! -d "$BUNDLE_DIR" ]; then
  acbh_vps_die "bundle directory not found: $BUNDLE_DIR"
fi

BUNDLE_DIR="$(acbh_vps_resolve_path "$BUNDLE_DIR")"
ACBH_VPS_UPGRADE_DRY_RUN="$DRY_RUN"

VERSION=""
PREVIOUS_RELEASE=""
PREVIOUS_VERSION=""
STATE_BACKUP=""
STAGING_DIR=""
RELEASE_DIR=""
ROLLBACK_NEEDED="false"

cleanup_on_exit() {
  local exit_code=$?
  acbh_vps_release_upgrade_lock

  if [ "$ROLLBACK_NEEDED" = "true" ] && [ -n "$PREVIOUS_RELEASE" ] && [ -d "$PREVIOUS_RELEASE" ]; then
    if acbh_vps_perform_rollback "$PREVIOUS_RELEASE" "$PREVIOUS_VERSION" "$STATE_BACKUP"; then
      acbh_vps_write_upgrade_report "$VERSION" "$PREVIOUS_VERSION" "rollback_success" "${ACBH_VPS_UPGRADE_REPORT_ERROR:-rollback triggered}"
      exit 1
    fi
    acbh_vps_write_upgrade_report "$VERSION" "$PREVIOUS_VERSION" "rollback_failed" "${ACBH_VPS_UPGRADE_REPORT_ERROR:-rollback failed}"
    exit 1
  fi

  if [ "$exit_code" -ne 0 ] && [ -n "$VERSION" ]; then
    acbh_vps_write_upgrade_report "$VERSION" "$PREVIOUS_VERSION" "failed" "${ACBH_VPS_UPGRADE_REPORT_ERROR:-upgrade failed}"
  fi

  acbh_vps_cleanup_staging_dir "$STAGING_DIR"
  exit "$exit_code"
}
trap cleanup_on_exit EXIT

acbh_vps_ensure_layout
acbh_vps_migrate_legacy_layout
acbh_vps_acquire_upgrade_lock

VERSION="$(acbh_vps_read_bundle_version "$BUNDLE_DIR")"
RELEASE_DIR="$(acbh_vps_release_dir_for_version "$VERSION")"
STAGING_DIR="$(acbh_vps_staging_dir_for_version "$VERSION")"

acbh_vps_validate_bundle_structure "$BUNDLE_DIR"
acbh_vps_validate_bundle_checksums "$BUNDLE_DIR"
acbh_vps_validate_node_version
acbh_vps_check_disk_space "$ACBH_INSTALL_DIR"

if [ -e "$RELEASE_DIR" ]; then
  current_version=""
  if current_dir="$(acbh_vps_current_release_dir 2>/dev/null || true)"; then
    current_version="$(acbh_vps_release_version_path "$current_dir")"
  fi
  if [ -n "$current_version" ] && \
    [ "$(acbh_vps_normalize_version "$current_version")" = "$(acbh_vps_normalize_version "$VERSION")" ]; then
    acbh_vps_log "release $VERSION is already current; verifying health"
    acbh_vps_assert_health "$VERSION"
    acbh_vps_assert_ports
    PREVIOUS_VERSION="$(acbh_vps_read_upgrade_state_previous 2>/dev/null || true)"
    acbh_vps_write_upgrade_report "$VERSION" "$PREVIOUS_VERSION" "success"
    trap - EXIT
    acbh_vps_release_upgrade_lock
    exit 0
  fi
  acbh_vps_die "release directory already exists: $RELEASE_DIR"
fi

if [ -d "$STAGING_DIR" ]; then
  acbh_vps_cleanup_staging_dir "$STAGING_DIR"
fi

if current_dir="$(acbh_vps_current_release_dir 2>/dev/null || true)"; then
  PREVIOUS_RELEASE="$current_dir"
  PREVIOUS_VERSION="$(acbh_vps_release_version_path "$current_dir")"
else
  PREVIOUS_RELEASE=""
  PREVIOUS_VERSION=""
fi

if [ "$DRY_RUN" = "true" ]; then
  acbh_vps_log "dry-run: bundle $VERSION validated; no changes applied"
  acbh_vps_write_upgrade_report "$VERSION" "$PREVIOUS_VERSION" "success"
  trap - EXIT
  acbh_vps_release_upgrade_lock
  exit 0
fi

acbh_vps_copy_bundle_to_dir "$BUNDLE_DIR" "$STAGING_DIR"
acbh_vps_validate_release_dir "$STAGING_DIR"
mv "$STAGING_DIR" "$RELEASE_DIR"
STAGING_DIR=""

STATE_BACKUP="$(acbh_vps_backup_coordinator_state || true)"
ACBH_VPS_STATE_BACKUP="$STATE_BACKUP"
ROLLBACK_NEEDED="true"

acbh_vps_service_stop
acbh_vps_switch_current_symlink "$RELEASE_DIR"
acbh_vps_service_start

if ! acbh_vps_wait_for_health "$VERSION"; then
  ACBH_VPS_UPGRADE_REPORT_ERROR="health check failed after upgrade (attempts=${ACBH_VPS_HEALTH_WAIT_ATTEMPTS:-0}, elapsed=${ACBH_VPS_HEALTH_WAIT_ELAPSED:-0}s, last=${ACBH_VPS_HEALTH_WAIT_LAST_ERROR:-unknown})"
  acbh_vps_die "$ACBH_VPS_UPGRADE_REPORT_ERROR"
fi
if ! acbh_vps_check_ports; then
  ACBH_VPS_UPGRADE_REPORT_ERROR="port check failed after upgrade"
  acbh_vps_die "$ACBH_VPS_UPGRADE_REPORT_ERROR"
fi

ROLLBACK_NEEDED="false"
acbh_vps_write_upgrade_state "$VERSION" "$PREVIOUS_VERSION"
acbh_vps_prune_releases
acbh_vps_write_systemd_unit
acbh_vps_write_upgrade_report "$VERSION" "$PREVIOUS_VERSION" "success"

trap - EXIT
acbh_vps_release_upgrade_lock
acbh_vps_log "upgrade to $VERSION complete"