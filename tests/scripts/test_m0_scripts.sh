#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT
BASE_PATH=$PATH

PASS_COUNT=0

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local haystack=$1
  local needle=$2
  [[ "$haystack" == *"$needle"* ]] || fail "expected output to contain: $needle"
}

assert_file_size() {
  local path=$1
  local expected=$2
  local actual
  actual=$(stat -f %z "$path")
  [[ "$actual" == "$expected" ]] || fail "$path size was $actual, expected $expected"
}

run_test() {
  local name=$1
  shift
  "$@"
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'ok %d - %s\n' "$PASS_COUNT" "$name"
}

setup_fake_esptool() {
  mkdir -p "$TMP_DIR/bin"
  cat >"$TMP_DIR/bin/esptool" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$ESPTOOL_LOG"

case " $* " in
  *" version "*)
    printf 'esptool.py v4.12.dev3\n'
    ;;
  *" read_mac "*)
    printf 'Chip is ESP32-S3\nMAC: aa:bb:cc:dd:ee:ff\n'
    ;;
  *" flash_id "*)
    printf 'Detected flash size: 16MB\n'
    ;;
  *" read_flash "*)
    output=${!#}
    dd if=/dev/zero of="$output" bs=1 count=0 seek=16777216 2>/dev/null
    ;;
  *" write_flash "*)
    printf 'write complete\n'
    ;;
esac
EOF
  chmod +x "$TMP_DIR/bin/esptool"
  cat >"$TMP_DIR/bin/idf.py" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${IDF_TOOL_LOG:-}" ]]; then
  printf '%s\n' "$*" >>"$IDF_TOOL_LOG"
fi
if [[ " $* " == *" --version "* ]]; then
  printf 'ESP-IDF v5.5.4\n'
fi
EOF
  chmod +x "$TMP_DIR/bin/idf.py"
  export PATH="$TMP_DIR/bin:$PATH"
  export ESPTOOL_LOG="$TMP_DIR/esptool.log"
  export IDF_TOOL_LOG="$TMP_DIR/idf-tool.log"
  : >"$ESPTOOL_LOG"
  : >"$IDF_TOOL_LOG"
}

test_detects_single_device() {
  local dev_root="$TMP_DIR/dev-single"
  mkdir -p "$dev_root"
  touch "$dev_root/cu.usbmodem101"

  local output
  output=$(AGENT_BEACON_DEV_ROOT="$dev_root" "$ROOT_DIR/scripts/detect-device.sh")
  assert_contains "$output" "PORT=$dev_root/cu.usbmodem101"
  assert_contains "$(cat "$ESPTOOL_LOG")" "read_mac"
  assert_contains "$(cat "$ESPTOOL_LOG")" "flash_id"
}

test_rejects_ambiguous_devices() {
  local dev_root="$TMP_DIR/dev-multiple"
  mkdir -p "$dev_root"
  touch "$dev_root/cu.usbmodem101" "$dev_root/cu.usbmodem202"

  local output
  if output=$(AGENT_BEACON_DEV_ROOT="$dev_root" "$ROOT_DIR/scripts/detect-device.sh" 2>&1); then
    fail "detect-device should reject multiple devices"
  fi
  assert_contains "$output" "$dev_root/cu.usbmodem101"
  assert_contains "$output" "$dev_root/cu.usbmodem202"
  assert_contains "$output" "--port"
}

test_backup_creates_verified_external_copy() {
  local backup="$TMP_DIR/backups/factory.bin"
  local external="$TMP_DIR/external"
  mkdir -p "$(dirname "$backup")"

  local output
  output=$(
    "$ROOT_DIR/scripts/backup-factory.sh" \
      --port /dev/cu.usbmodem101 \
      --output "$backup" \
      --external-dir "$external"
  )

  assert_file_size "$backup" 16777216
  assert_file_size "$external/factory.bin" 16777216
  [[ "$(shasum -a 256 "$backup" | awk '{print $1}')" == \
     "$(shasum -a 256 "$external/factory.bin" | awk '{print $1}')" ]] || \
    fail "backup copies must have matching SHA-256"
  assert_contains "$output" "SIZE=16777216"
  assert_contains "$(cat "$ESPTOOL_LOG")" "read_flash 0 ALL $backup"
}

test_restore_defaults_to_dry_run() {
  local backup="$TMP_DIR/factory-valid.bin"
  dd if=/dev/zero of="$backup" bs=1 count=0 seek=16777216 2>/dev/null
  : >"$ESPTOOL_LOG"

  local output
  output=$(
    "$ROOT_DIR/scripts/restore-factory.sh" \
      --port /dev/cu.usbmodem101 \
      --backup "$backup"
  )

  assert_contains "$output" "DRY RUN"
  [[ ! -s "$ESPTOOL_LOG" ]] || fail "dry-run must not call esptool"
}

test_restore_requires_yes_and_valid_size() {
  local backup="$TMP_DIR/factory-valid.bin"
  dd if=/dev/zero of="$backup" bs=1 count=0 seek=16777216 2>/dev/null
  : >"$ESPTOOL_LOG"

  "$ROOT_DIR/scripts/restore-factory.sh" \
    --port /dev/cu.usbmodem101 \
    --backup "$backup" \
    --yes >/dev/null
  assert_contains "$(cat "$ESPTOOL_LOG")" "write_flash 0 $backup"

  local invalid="$TMP_DIR/factory-invalid.bin"
  printf x >"$invalid"
  if "$ROOT_DIR/scripts/restore-factory.sh" \
    --port /dev/cu.usbmodem101 \
    --backup "$invalid" \
    --yes >/dev/null 2>&1; then
    fail "restore should reject a backup that is not 16 MB"
  fi
}

test_doctor_reports_pinned_environment() {
  local output
  output=$(IDF_ACTIVATION_SCRIPT="$TMP_DIR/missing-activation.sh" "$ROOT_DIR/scripts/doctor.sh")
  assert_contains "$output" "ESP-IDF v5.5.4"
  assert_contains "$output" "esptool.py v4.12.dev3"
}

test_detect_uses_eim_python_fallback() {
  local dev_root="$TMP_DIR/dev-eim"
  local idf_path="$TMP_DIR/fake-idf"
  local python_env="$TMP_DIR/fake-python-env"
  mkdir -p "$dev_root" "$idf_path/components/esptool_py/esptool" "$python_env/bin"
  touch "$dev_root/cu.usbmodem303"
  touch "$idf_path/components/esptool_py/esptool/esptool.py"

  cat >"$python_env/bin/python" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$ESPTOOL_LOG"
case " $* " in
  *" read_mac "*)
    printf 'Chip is ESP32-S3\nMAC: aa:bb:cc:dd:ee:ff\n'
    ;;
  *" flash_id "*)
    printf 'Detected flash size: 16MB\n'
    ;;
esac
EOF
  chmod +x "$python_env/bin/python"
  : >"$ESPTOOL_LOG"

  local output
  output=$(
    PATH="/usr/bin:/bin" \
      IDF_PATH="$idf_path" \
      IDF_PYTHON_ENV_PATH="$python_env" \
      AGENT_BEACON_DEV_ROOT="$dev_root" \
      "$ROOT_DIR/scripts/detect-device.sh"
  )
  assert_contains "$output" "PORT=$dev_root/cu.usbmodem303"
  assert_contains "$(cat "$ESPTOOL_LOG")" "$idf_path/components/esptool_py/esptool/esptool.py"
  assert_contains "$(cat "$ESPTOOL_LOG")" "read_mac"
}

test_doctor_reads_eim_environment_without_sourcing() {
  local activation="$TMP_DIR/fake-activation.sh"
  local idf_path="$TMP_DIR/doctor-idf"
  local python_env="$TMP_DIR/doctor-python-env"
  local tools_path="$TMP_DIR/doctor-tools"
  mkdir -p "$idf_path/tools" "$idf_path/components/esptool_py/esptool" "$python_env/bin" "$tools_path"
  touch "$idf_path/tools/idf.py" "$idf_path/components/esptool_py/esptool/esptool.py"

  cat >"$activation" <<EOF
if [[ \${1:-} != "-e" ]]; then
  exit 91
fi
printf 'IDF_PATH=%s\n' '$idf_path'
printf 'IDF_PYTHON_ENV_PATH=%s\n' '$python_env'
printf 'IDF_TOOLS_PATH=%s\n' '$tools_path'
EOF
  cat >"$python_env/bin/python" <<'EOF'
#!/usr/bin/env bash
[[ -n "${IDF_TOOLS_PATH:-}" ]] || exit 92
case "$1" in
  */tools/idf.py)
    printf 'ESP-IDF v5.5.4\n'
    ;;
  */components/esptool_py/esptool/esptool.py)
    printf 'esptool.py v4.12.dev3\n'
    ;;
esac
EOF
  chmod +x "$python_env/bin/python"

  local output
  output=$(PATH="$BASE_PATH" IDF_ACTIVATION_SCRIPT="$activation" "$ROOT_DIR/scripts/doctor.sh")
  assert_contains "$output" "doctor: environment matches DEV.md"
}

test_flash_refuses_without_factory_backup() {
  local firmware_dir="$TMP_DIR/firmware-no-backup"
  local backup_dir="$TMP_DIR/no-backups"
  mkdir -p "$firmware_dir" "$backup_dir"

  local output
  if output=$(
    "$ROOT_DIR/scripts/flash-firmware.sh" \
      --port /dev/cu.usbmodem101 \
      --firmware-dir "$firmware_dir" \
      --backup-dir "$backup_dir" 2>&1
  ); then
    fail "flash-firmware should refuse to write without a factory backup"
  fi
  assert_contains "$output" "valid 16 MB factory backup"
}

test_flash_runs_with_factory_backup() {
  local firmware_dir="$TMP_DIR/firmware-with-backup"
  local backup_dir="$TMP_DIR/valid-backups"
  mkdir -p "$firmware_dir" "$backup_dir"
  dd if=/dev/zero of="$backup_dir/factory-test.bin" bs=1 count=0 seek=16777216 2>/dev/null
  : >"$IDF_TOOL_LOG"

  "$ROOT_DIR/scripts/flash-firmware.sh" \
    --port /dev/cu.usbmodem101 \
    --firmware-dir "$firmware_dir" \
    --backup-dir "$backup_dir" >/dev/null
  assert_contains "$(cat "$IDF_TOOL_LOG")" "-p /dev/cu.usbmodem101 flash"
}

test_monitor_uses_selected_port() {
  local firmware_dir="$TMP_DIR/firmware-monitor"
  mkdir -p "$firmware_dir"
  : >"$IDF_TOOL_LOG"

  "$ROOT_DIR/scripts/monitor.sh" \
    --port /dev/cu.usbmodem101 \
    --firmware-dir "$firmware_dir" >/dev/null
  assert_contains "$(cat "$IDF_TOOL_LOG")" "-p /dev/cu.usbmodem101 monitor"
}

setup_fake_esptool
run_test "detects one USB modem" test_detects_single_device
run_test "rejects ambiguous USB modems" test_rejects_ambiguous_devices
run_test "backs up and verifies an external copy" test_backup_creates_verified_external_copy
run_test "restore is dry-run by default" test_restore_defaults_to_dry_run
run_test "restore needs --yes and a 16 MB image" test_restore_requires_yes_and_valid_size
run_test "doctor reports the pinned toolchain" test_doctor_reports_pinned_environment
run_test "detect supports EIM's Python esptool entrypoint" test_detect_uses_eim_python_fallback
run_test "doctor reads EIM environment without sourcing" test_doctor_reads_eim_environment_without_sourcing
run_test "flash refuses without a factory backup" test_flash_refuses_without_factory_backup
run_test "flash runs with a valid factory backup" test_flash_runs_with_factory_backup
run_test "monitor uses the selected serial port" test_monitor_uses_selected_port

printf '1..%d\n' "$PASS_COUNT"
