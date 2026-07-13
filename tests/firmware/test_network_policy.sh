#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$BUILD_DIR"' EXIT

grep -qx 'CONFIG_WS_BUFFER_SIZE=4096' "$ROOT_DIR/firmware/sdkconfig.defaults"

cc \
  -std=c11 \
  -Wall \
  -Wextra \
  -Werror \
  -I"$ROOT_DIR/firmware/components/beacon_network/include" \
  "$ROOT_DIR/firmware/test/test_network_policy.c" \
  "$ROOT_DIR/firmware/components/beacon_network/beacon_network_policy.c" \
  -o "$BUILD_DIR/test_network_policy"

"$BUILD_DIR/test_network_policy"
printf 'Network policy tests passed\n'
