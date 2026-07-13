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
  -I"$ROOT_DIR/firmware/components/beacon_calibration/include" \
  "$ROOT_DIR/firmware/test/test_calibration.c" \
  "$ROOT_DIR/firmware/components/beacon_calibration/beacon_calibration.c" \
  -o "$BUILD_DIR/test_calibration"

"$BUILD_DIR/test_calibration"
printf 'calibration tests passed\n'
