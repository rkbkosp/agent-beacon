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
  -I"$ROOT_DIR/firmware/components/board_ws_147b/include" \
  "$ROOT_DIR/firmware/test/test_board_geometry.c" \
  "$ROOT_DIR/firmware/components/board_ws_147b/board_ws_147b_geometry.c" \
  -o "$BUILD_DIR/test_board_geometry"

"$BUILD_DIR/test_board_geometry"
printf 'board geometry tests passed\n'
