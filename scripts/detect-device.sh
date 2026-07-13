#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source "$SCRIPT_DIR/lib/idf-tools.sh"

usage() {
  cat <<'EOF'
Usage: scripts/detect-device.sh [--port /dev/cu.usbmodemXXXX]

Detect an ESP32-S3 USB serial device and print its chip and Flash details.
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

port=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --port)
      [[ $# -ge 2 ]] || die "--port requires a value"
      port=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

configure_esptool || \
  die "esptool is not active; source ~/.espressif/tools/activate_idf_v5.5.4.sh"

if [[ -z "$port" ]]; then
  dev_root=${AGENT_BEACON_DEV_ROOT:-/dev}
  shopt -s nullglob
  candidates=("$dev_root"/cu.usbmodem*)
  shopt -u nullglob

  case ${#candidates[@]} in
    0)
      die "no cu.usbmodem device found; reconnect the data cable or enter BOOT download mode"
      ;;
    1)
      port=${candidates[0]}
      ;;
    *)
      printf 'error: multiple USB modem devices found:\n' >&2
      printf '  %s\n' "${candidates[@]}" >&2
      printf 'rerun with --port <device> to select one explicitly\n' >&2
      exit 2
      ;;
  esac
fi

printf 'PORT=%s\n' "$port"
run_esptool -p "$port" read_mac
run_esptool -p "$port" flash_id
