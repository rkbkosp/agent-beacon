#!/usr/bin/env bash

set -euo pipefail

readonly EXPECTED_FLASH_BYTES=16777216
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
source "$SCRIPT_DIR/lib/idf-tools.sh"

usage() {
  cat <<'EOF'
Usage: scripts/backup-factory.sh --port DEVICE [options]

Options:
  --output FILE          Backup path (default: backups/factory-TIMESTAMP.bin)
  --external-dir DIR     Verified second-copy directory
                         (default: ~/Documents/AgentBeaconBackups)
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

port=${PORT:-}
output=""
external_dir=${AGENT_BEACON_BACKUP_DIR:-"$HOME/Documents/AgentBeaconBackups"}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port)
      [[ $# -ge 2 ]] || die "--port requires a value"
      port=$2
      shift 2
      ;;
    --output)
      [[ $# -ge 2 ]] || die "--output requires a value"
      output=$2
      shift 2
      ;;
    --external-dir)
      [[ $# -ge 2 ]] || die "--external-dir requires a value"
      external_dir=$2
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
configure_esptool || \
  die "esptool is not active; source ~/.espressif/tools/activate_idf_v5.5.4.sh"

if [[ -z "$output" ]]; then
  output="$ROOT_DIR/backups/factory-$(date +%Y%m%d-%H%M%S).bin"
fi

[[ ! -e "$output" ]] || die "refusing to overwrite existing backup: $output"
mkdir -p "$(dirname "$output")" "$external_dir"

cleanup_incomplete() {
  if [[ -f "$output" ]]; then
    local size
    size=$(stat -f %z "$output" 2>/dev/null || printf 0)
    if [[ "$size" != "$EXPECTED_FLASH_BYTES" ]]; then
      rm -f "$output"
    fi
  fi
}
trap cleanup_incomplete EXIT

run_esptool -p "$port" -b 460800 read_flash 0 ALL "$output"

size=$(stat -f %z "$output")
[[ "$size" == "$EXPECTED_FLASH_BYTES" ]] || \
  die "backup size is $size bytes; expected $EXPECTED_FLASH_BYTES"

external_copy="$external_dir/$(basename "$output")"
[[ ! -e "$external_copy" ]] || die "refusing to overwrite external copy: $external_copy"
cp -p "$output" "$external_copy"

repo_sha=$(shasum -a 256 "$output" | awk '{print $1}')
external_sha=$(shasum -a 256 "$external_copy" | awk '{print $1}')
[[ "$repo_sha" == "$external_sha" ]] || die "external copy SHA-256 mismatch"

trap - EXIT
printf 'BACKUP=%s\nEXTERNAL=%s\nSIZE=%s\nSHA256=%s\n' \
  "$output" "$external_copy" "$size" "$repo_sha"
