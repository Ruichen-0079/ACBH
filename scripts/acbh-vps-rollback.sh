#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=acbh-vps-lib.sh
. "$SCRIPT_DIR/acbh-vps-lib.sh"

TARGET_VERSION=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir) ACBH_INSTALL_DIR="${2:-}"; shift 2 ;;
    -h|--help)
      cat <<'EOF'
usage: sudo bash acbh-vps-rollback.sh [version]
EOF
      exit 0
      ;;
    -*)
      acbh_vps_die "unknown argument: $1"
      ;;
    *)
      if [ -z "$TARGET_VERSION" ]; then
        TARGET_VERSION="$1"
      else
        acbh_vps_die "unexpected argument: $1"
      fi
      shift
      ;;
  esac
done

acbh_vps_require_root
acbh_vps_ensure_layout
acbh_vps_acquire_upgrade_lock

CURRENT_VERSION=""
CURRENT_RELEASE=""
TARGET_RELEASE=""
TARGET_VERSION_EFFECTIVE=""
PREVIOUS_VERSION=""

cleanup_on_exit() {
  acbh_vps_release_upgrade_lock
}
trap cleanup_on_exit EXIT

if ! CURRENT_RELEASE="$(acbh_vps_current_release_dir)"; then
  acbh_vps_die "no current release configured"
fi
CURRENT_VERSION="$(acbh_vps_release_version_path "$CURRENT_RELEASE")"

if [ -z "$TARGET_VERSION" ]; then
  if ! TARGET_VERSION="$(acbh_vps_read_upgrade_state_previous 2>/dev/null || true)"; then
    acbh_vps_die "no previous release recorded; specify a version explicitly"
  fi
  if [ -z "$TARGET_VERSION" ]; then
    acbh_vps_die "no previous release recorded; specify a version explicitly"
  fi
fi

acbh_vps_validate_version_string "$TARGET_VERSION"
TARGET_RELEASE="$(acbh_vps_release_dir_for_version "$TARGET_VERSION")"
if [ ! -d "$TARGET_RELEASE" ]; then
  acbh_vps_die "release not found: $TARGET_VERSION"
fi
TARGET_VERSION_EFFECTIVE="$(acbh_vps_release_version_path "$TARGET_RELEASE")"

if [ "$CURRENT_RELEASE" = "$TARGET_RELEASE" ]; then
  acbh_vps_log "already on release $TARGET_VERSION_EFFECTIVE"
  trap - EXIT
  acbh_vps_release_upgrade_lock
  exit 0
fi

acbh_vps_log "rolling back from $CURRENT_VERSION to $TARGET_VERSION_EFFECTIVE"
acbh_vps_service_stop
acbh_vps_switch_current_symlink "$TARGET_RELEASE"
acbh_vps_service_start
acbh_vps_assert_health "$TARGET_VERSION_EFFECTIVE"
acbh_vps_assert_ports

PREVIOUS_VERSION="$CURRENT_VERSION"
acbh_vps_write_upgrade_state "$TARGET_VERSION_EFFECTIVE" "$PREVIOUS_VERSION"
acbh_vps_write_upgrade_report "$TARGET_VERSION_EFFECTIVE" "$PREVIOUS_VERSION" "rollback_success"

trap - EXIT
acbh_vps_release_upgrade_lock
acbh_vps_log "rollback to $TARGET_VERSION_EFFECTIVE complete"