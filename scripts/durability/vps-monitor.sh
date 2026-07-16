#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-run}"
OUTPUT_DIR="${ACBH_MONITOR_OUTPUT_DIR:-/var/log/acbh-v04-test/durability}"
INTERVAL_SECONDS="${ACBH_MONITOR_INTERVAL_SECONDS:-60}"
MAX_BYTES="${ACBH_MONITOR_MAX_BYTES:-20971520}"
MAX_FILES="${ACBH_MONITOR_MAX_FILES:-5}"
SUMMARY_PATH="${ACBH_MONITOR_SUMMARY_PATH:-${OUTPUT_DIR}/vps-summary.json}"

mkdir -p -- "$OUTPUT_DIR"

is_active() {
  systemctl is-active "$1" 2>/dev/null || true
}

listener_present() {
  ss -ltnH "sport = :$1" 2>/dev/null | grep -q .
}

tcp_probe() {
  timeout 3 bash -c "</dev/tcp/127.0.0.1/$1" >/dev/null 2>&1
}

process_metric() {
  local pid="$1" field="$2"
  if [[ "$pid" =~ ^[1-9][0-9]*$ ]] && kill -0 "$pid" 2>/dev/null; then
    ps -o "${field}=" -p "$pid" 2>/dev/null | tr -d ' ' || true
  else
    printf '0'
  fi
}

fd_count() {
  local pid="$1"
  if [[ "$pid" =~ ^[1-9][0-9]*$ ]] && [[ -d "/proc/$pid/fd" ]]; then
    find "/proc/$pid/fd" -mindepth 1 -maxdepth 1 2>/dev/null | wc -l
  else
    printf '0'
  fi
}

rotate_log() {
  local current
  current="$(find "$OUTPUT_DIR" -maxdepth 1 -type f -name 'vps-monitor-*.jsonl' -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -n1 | cut -d' ' -f2- || true)"
  if [[ -z "$current" ]] || (( $(stat -c %s "$current" 2>/dev/null || printf '%s' "$MAX_BYTES") >= MAX_BYTES )); then
    current="$OUTPUT_DIR/vps-monitor-$(date -u +%Y%m%d-%H%M%S)-$$.jsonl"
    : >"$current"
  fi
  mapfile -t files < <(find "$OUTPUT_DIR" -maxdepth 1 -type f -name 'vps-monitor-*.jsonl' -printf '%T@ %p\n' | sort -nr | cut -d' ' -f2-)
  if (( ${#files[@]} > MAX_FILES )); then
    local index
    for ((index=MAX_FILES; index<${#files[@]}; index++)); do
      rm -f -- "${files[$index]}"
    done
  fi
  printf '%s' "$current"
}

write_sample() {
  local coordinator_state frps_state coordinator_pid frps_pid
  local player_listener coordinator_listener frps_listener player_probe relay_state
  local coordinator_rss coordinator_threads coordinator_fds frps_rss frps_threads frps_fds
  coordinator_state="$(is_active acbh-v04-test-server.service)"
  frps_state="$(is_active acbh-v04-test-frps.service)"
  coordinator_pid="$(systemctl show -p MainPID --value acbh-v04-test-server.service 2>/dev/null || printf '0')"
  frps_pid="$(systemctl show -p MainPID --value acbh-v04-test-frps.service 2>/dev/null || printf '0')"
  if listener_present 25575; then player_listener=true; else player_listener=false; fi
  if listener_present 6122; then coordinator_listener=true; else coordinator_listener=false; fi
  if listener_present 7001; then frps_listener=true; else frps_listener=false; fi
  if tcp_probe 25575; then player_probe=true; else player_probe=false; fi
  if [[ "$frps_state" == active && "$player_listener" == true && "$player_probe" == true ]]; then
    relay_state=ONLINE
  else
    relay_state=DEGRADED
  fi
  coordinator_rss="$(process_metric "$coordinator_pid" rss)"
  coordinator_threads="$(process_metric "$coordinator_pid" nlwp)"
  coordinator_fds="$(fd_count "$coordinator_pid")"
  frps_rss="$(process_metric "$frps_pid" rss)"
  frps_threads="$(process_metric "$frps_pid" nlwp)"
  frps_fds="$(fd_count "$frps_pid")"
  local log_bytes
  log_bytes="$(du -sb /var/log/acbh-v04-test 2>/dev/null | awk '{print $1}' || printf '0')"
  printf '{"timestamp_utc":"%s","agent_state":"observed_on_windows_host","minecraft_state":"observed_on_windows_host","relay_state":"%s","coordinator_state":"%s","frps_state":"%s","java_process_count":%s,"frpc_process_count":%s,"coordinator_pid":%s,"coordinator_working_set_bytes":%s,"coordinator_thread_count":%s,"coordinator_fd_count":%s,"frps_pid":%s,"frps_working_set_bytes":%s,"frps_thread_count":%s,"frps_fd_count":%s,"public_25575_listening":%s,"public_25575_reachable":%s,"coordinator_6122_listening":%s,"frps_7001_listening":%s,"protected_local_25565_state":"observed_on_windows_host","test_log_directory_bytes":%s}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$relay_state" "$coordinator_state" "$frps_state" \
    "$(pgrep -xc java 2>/dev/null || true)" "$(pgrep -xc frpc 2>/dev/null || true)" \
    "${coordinator_pid:-0}" "$(( ${coordinator_rss:-0} * 1024 ))" "${coordinator_threads:-0}" "${coordinator_fds:-0}" \
    "${frps_pid:-0}" "$(( ${frps_rss:-0} * 1024 ))" "${frps_threads:-0}" "${frps_fds:-0}" \
    "$player_listener" "$player_probe" "$coordinator_listener" "$frps_listener" "${log_bytes:-0}" \
    >>"$(rotate_log)"
}

collect_report() {
  python3 - "$OUTPUT_DIR" "$SUMMARY_PATH" <<'PY'
import glob
import json
import os
import sys
from datetime import datetime, timezone

directory, destination = sys.argv[1:]
records = []
for path in sorted(glob.glob(os.path.join(directory, "vps-monitor-*.jsonl"))):
    with open(path, "r", encoding="utf-8") as handle:
        for line in handle:
            if line.strip():
                records.append(json.loads(line))
if not records:
    raise SystemExit("no VPS durability samples found")
summary = {
    "generated_utc": datetime.now(timezone.utc).isoformat(),
    "sample_count": len(records),
    "first_sample_utc": records[0]["timestamp_utc"],
    "last_sample_utc": records[-1]["timestamp_utc"],
    "relay_non_online_samples": sum(r["relay_state"] != "ONLINE" for r in records),
    "coordinator_non_active_samples": sum(r["coordinator_state"] != "active" for r in records),
    "frps_non_active_samples": sum(r["frps_state"] != "active" for r in records),
    "public_probe_failures": sum(not r["public_25575_reachable"] for r in records),
    "max_coordinator_working_set_bytes": max(r["coordinator_working_set_bytes"] for r in records),
    "max_frps_working_set_bytes": max(r["frps_working_set_bytes"] for r in records),
}
with open(destination, "w", encoding="utf-8") as handle:
    json.dump(summary, handle, indent=2)
print(json.dumps(summary, indent=2))
PY
}

case "$MODE" in
  run)
    while true; do
      write_sample
      sleep "$INTERVAL_SECONDS"
    done
    ;;
  once) write_sample ;;
  collect) collect_report ;;
  *) printf 'usage: %s {run|once|collect}\n' "$0" >&2; exit 2 ;;
esac
