#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
IDF_ROOT=${IDF_PATH:-$HOME/.espressif/v5.5.4/esp-idf}
BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$BUILD_DIR"' EXIT

if [[ ! -f "$IDF_ROOT/components/json/cJSON/cJSON.c" ]]; then
  printf 'error: ESP-IDF cJSON source not found under %s\n' "$IDF_ROOT" >&2
  exit 1
fi

cc \
  -std=c11 \
  -Wall \
  -Wextra \
  -Werror \
  -I"$ROOT_DIR/firmware/components/beacon_protocol/include" \
  -I"$ROOT_DIR/firmware/components/beacon_notifications/include" \
  -I"$ROOT_DIR/firmware/components/beacon_state/include" \
  -I"$IDF_ROOT/components/json/cJSON" \
  "$ROOT_DIR/firmware/test/test_protocol_json.c" \
  "$ROOT_DIR/firmware/components/beacon_protocol/beacon_protocol_json.c" \
  "$ROOT_DIR/firmware/components/beacon_protocol/beacon_protocol_types.c" \
  "$ROOT_DIR/firmware/components/beacon_state/beacon_app_state.c" \
  "$IDF_ROOT/components/json/cJSON/cJSON.c" \
  -o "$BUILD_DIR/test_protocol_json"

"$BUILD_DIR/test_protocol_json"
printf 'Protocol JSON tests passed\n'
