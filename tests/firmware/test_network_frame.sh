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
  -I"$ROOT_DIR/firmware/components/beacon_network/include" \
  "$ROOT_DIR/firmware/test/test_network_frame.c" \
  "$ROOT_DIR/firmware/components/beacon_network/beacon_network_frame.c" \
  -o "$BUILD_DIR/test_network_frame"

"$BUILD_DIR/test_network_frame"
printf 'Network frame tests passed\n'
