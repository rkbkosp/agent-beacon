#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
source "$SCRIPT_DIR/lib/idf-tools.sh"

usage() {
  cat <<'EOF'
Usage: scripts/monitor.sh --port DEVICE [--firmware-dir DIR]
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

port=${PORT:-}
firmware_dir="$ROOT_DIR/firmware"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port)
      [[ $# -ge 2 ]] || die "--port requires a value"
      port=$2
      shift 2
      ;;
    --firmware-dir)
      [[ $# -ge 2 ]] || die "--firmware-dir requires a value"
      firmware_dir=$2
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

[[ -n "$port" ]] || die "a serial port is required via --port or PORT"
[[ -d "$firmware_dir" ]] || die "firmware directory does not exist: $firmware_dir"
configure_idf || die "idf.py is unavailable; activate ESP-IDF v5.5.4 first"

cd "$firmware_dir"
run_idf -p "$port" monitor
