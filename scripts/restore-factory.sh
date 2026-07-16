#!/usr/bin/env bash

set -euo pipefail

readonly EXPECTED_FLASH_BYTES=16777216
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source "$SCRIPT_DIR/lib/idf-tools.sh"
source "$SCRIPT_DIR/lib/bridge-service.sh"

usage() {
  cat <<'EOF'
Usage: scripts/restore-factory.sh --port DEVICE --backup FILE [options]

Options:
  --expected-sha256 SHA  Require an exact backup digest before restore
  --yes                  Perform the write; without this flag, print a dry-run
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

port=${PORT:-}
backup=""
expected_sha=""
confirmed=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port)
      [[ $# -ge 2 ]] || die "--port requires a value"
      port=$2
      shift 2
      ;;
    --backup)
      [[ $# -ge 2 ]] || die "--backup requires a value"
      backup=$2
      shift 2
      ;;
    --expected-sha256)
      [[ $# -ge 2 ]] || die "--expected-sha256 requires a value"
      expected_sha=$2
      shift 2
      ;;
    --yes)
      confirmed=true
      shift
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
[[ -n "$backup" ]] || die "--backup is required"
[[ -f "$backup" ]] || die "backup does not exist: $backup"

size=$(stat -f %z "$backup")
[[ "$size" == "$EXPECTED_FLASH_BYTES" ]] || \
  die "backup size is $size bytes; expected $EXPECTED_FLASH_BYTES"

actual_sha=$(shasum -a 256 "$backup" | awk '{print $1}')
if [[ -n "$expected_sha" && "$actual_sha" != "$expected_sha" ]]; then
  die "backup SHA-256 mismatch: got $actual_sha"
fi

if [[ "$confirmed" != true ]]; then
  printf 'DRY RUN: validated 16 MB backup (SHA256=%s)\n' "$actual_sha"
  printf 'Would run: esptool -p %q -b 460800 write_flash 0 %q\n' "$port" "$backup"
  printf 'Rerun with --yes to write the device.\n'
  exit 0
fi

configure_esptool || \
  die "esptool is not active; source ~/.espressif/tools/activate_idf_v5.5.4.sh"

printf 'Restoring %s to %s (SHA256=%s)\n' "$backup" "$port" "$actual_sha"
bridge_service_pause_for_serial "$port"
trap 'bridge_service_resume || true' EXIT
run_esptool -p "$port" -b 460800 write_flash 0 "$backup"
