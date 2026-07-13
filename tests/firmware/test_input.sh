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
  -I"$ROOT_DIR/firmware/components/beacon_input/include" \
  "$ROOT_DIR/firmware/test/test_input.c" \
  "$ROOT_DIR/firmware/components/beacon_input/beacon_button.c" \
  -o "$BUILD_DIR/test_input"

"$BUILD_DIR/test_input"
printf 'input tests passed\n'
