#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$BUILD_DIR"' EXIT

cc \
  -std=c11 \
  -Wall \
  -Wextra \
  -Werror \
  -I"$ROOT_DIR/firmware/components/beacon_state/include" \
  -I"$ROOT_DIR/firmware/components/beacon_ui/include" \
  "$ROOT_DIR/firmware/test/test_ui_model.c" \
  "$ROOT_DIR/firmware/components/beacon_state/beacon_app_state.c" \
  "$ROOT_DIR/firmware/components/beacon_ui/beacon_ui_model.c" \
  -o "$BUILD_DIR/test_ui_model"

"$BUILD_DIR/test_ui_model"
printf 'UI model tests passed\n'
