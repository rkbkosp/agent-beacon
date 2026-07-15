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
  -I"$ROOT_DIR/firmware/components/beacon_diagnostics/include" \
  "$ROOT_DIR/firmware/test/test_diagnostics.c" \
  "$ROOT_DIR/firmware/components/beacon_diagnostics/beacon_diagnostics_math.c" \
  -o "$BUILD_DIR/test_diagnostics"

"$BUILD_DIR/test_diagnostics"
printf 'Diagnostics tests passed\n'
