#!/usr/bin/env bash

set -euo pipefail

readonly EXPECTED_FLASH_BYTES=16777216
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
source "$SCRIPT_DIR/lib/idf-tools.sh"

usage() {
  cat <<'EOF'
Usage: scripts/flash-firmware.sh --port DEVICE [options]

Options:
  --firmware-dir DIR  ESP-IDF project directory (default: firmware)
  --backup-dir DIR    Directory containing factory-*.bin (default: backups)
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

port=${PORT:-}
firmware_dir="$ROOT_DIR/firmware"
backup_dir="$ROOT_DIR/backups"

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
    --backup-dir)
      [[ $# -ge 2 ]] || die "--backup-dir requires a value"
      backup_dir=$2
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

shopt -s nullglob
backup_candidates=("$backup_dir"/factory-*.bin)
shopt -u nullglob

valid_backup=""
for candidate in "${backup_candidates[@]}"; do
  if [[ "$(stat -f %z "$candidate" 2>/dev/null || printf 0)" == "$EXPECTED_FLASH_BYTES" ]]; then
    valid_backup=$candidate
    break
  fi
done
[[ -n "$valid_backup" ]] || \
  die "no valid 16 MB factory backup found in: $backup_dir"

configure_idf || die "idf.py is unavailable; activate ESP-IDF v5.5.4 first"
printf 'Factory backup guard: %s (SHA256=%s)\n' \
  "$valid_backup" "$(shasum -a 256 "$valid_backup" | awk '{print $1}')"
printf 'Flashing firmware from %s to %s\n' "$firmware_dir" "$port"
(
  cd "$firmware_dir"
  run_idf -p "$port" flash
)
